package pcscommand

import (
	"bytes"
	"container/list"
	"encoding/hex"
	"fmt"
	"github.com/iikira/BaiduPCS-Go/baidupcs"
	"github.com/iikira/BaiduPCS-Go/baidupcs/pcserror"
	"github.com/iikira/BaiduPCS-Go/internal/pcsfunctions/pcsupload"
	"github.com/iikira/BaiduPCS-Go/pcscache"
	"github.com/iikira/BaiduPCS-Go/pcsutil"
	"github.com/iikira/BaiduPCS-Go/pcsutil/checksum"
	"github.com/iikira/BaiduPCS-Go/pcsutil/converter"
	"github.com/iikira/BaiduPCS-Go/pcsutil/delay"
	"github.com/iikira/BaiduPCS-Go/requester/rio"
	"github.com/iikira/BaiduPCS-Go/requester/uploader"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	requiredSliceSize = 256 * converter.KB // 256 KB
)

type (
	UploadOptions struct {
		NotRapidUpload bool
		NotFixMD5      bool
		NotSplitFile   bool // 禁用分片上传
	}

	// StepUpload 上传步骤
	StepUpload int

	utask struct {
		ListTask
		uploadInfo        *checksum.LocalFile // 要上传的本地文件详情
		step              StepUpload
		savePath          string
		uploadedDelayChan <-chan struct{} // 非强一致接口, 上传完成后需要等待
	}
)

const (
	// StepUploadInit 初始化步骤
	StepUploadInit StepUpload = iota
	// StepUploadRapidUpload 秒传步骤
	StepUploadRapidUpload
	// StepUploadUpload 正常上传步骤
	StepUploadUpload
)

// RunRapidUpload 执行秒传文件, 前提是知道文件的大小, md5, 前256KB切片的 md5, crc32
func RunRapidUpload(targetPath, contentMD5, sliceMD5, crc32 string, length int64) {
	targetPath, err := getAbsPath(targetPath)
	if err != nil {
		fmt.Printf("警告: %s, 获取网盘路径 %s 错误, %s\n", baidupcs.OperationRapidUpload, targetPath, err)
	}

	if sliceMD5 == "" {
		sliceMD5 = baidupcs.FixSliceMD5(sliceMD5)
	}

	err = GetBaiduPCS().RapidUpload(targetPath, contentMD5, sliceMD5, crc32, length)
	if err != nil {
		fmt.Printf("%s失败, 消息: %s\n", baidupcs.OperationRapidUpload, err)
		return
	}

	fmt.Printf("%s成功, 保存到网盘路径: %s\n", baidupcs.OperationRapidUpload, targetPath)
	return
}

// RunCreateSuperFile 执行分片上传—合并分片文件
func RunCreateSuperFile(targetPath string, blockList ...string) {
	targetPath, err := getAbsPath(targetPath)
	if err != nil {
		fmt.Printf("警告: %s, 获取网盘路径 %s 错误, %s\n", baidupcs.OperationUploadCreateSuperFile, targetPath, err)
	}

	err = GetBaiduPCS().UploadCreateSuperFile(targetPath, blockList...)
	if err != nil {
		fmt.Printf("%s失败, 消息: %s\n", baidupcs.OperationUploadCreateSuperFile, err)
		return
	}

	fmt.Printf("%s成功, 保存到网盘路径: %s\n", baidupcs.OperationUploadCreateSuperFile, targetPath)
	return
}

// RunUpload 执行文件上传
func RunUpload(localPaths []string, savePath string, opt *UploadOptions) {
	if opt == nil {
		opt = &UploadOptions{}
	}

	absSavePath, err := getAbsPath(savePath)
	if err != nil {
		fmt.Printf("警告: 上传文件, 获取网盘路径 %s 错误, %s\n", savePath, err)
	}

	switch len(localPaths) {
	case 0:
		fmt.Printf("本地路径为空\n")
		return
	}

	var (
		pcs           = GetBaiduPCS()
		ulist         = list.New()
		needsFixList  = list.New()
		lastID        int
		globedPathDir string
		subSavePath   string
	)

	for k := range localPaths {
		globedPaths, err := filepath.Glob(localPaths[k])
		if err != nil {
			fmt.Printf("上传文件, 匹配本地路径失败, %s\n", err)
			continue
		}

		for k2 := range globedPaths {
			walkedFiles, err := pcsutil.WalkDir(globedPaths[k2], "")
			if err != nil {
				fmt.Printf("警告: %s\n", err)
				continue
			}

			for k3 := range walkedFiles {
				// 针对 windows 的目录处理
				if os.PathSeparator == '\\' {
					walkedFiles[k3] = pcsutil.ConvertToUnixPathSeparator(walkedFiles[k3])
					globedPathDir = pcsutil.ConvertToUnixPathSeparator(filepath.Dir(globedPaths[k2]))
				} else {
					globedPathDir = filepath.Dir(globedPaths[k2])
				}

				// 避免去除文件名开头的"."
				if globedPathDir == "." {
					globedPathDir = ""
				}

				subSavePath = strings.TrimPrefix(walkedFiles[k3], globedPathDir)

				lastID++
				ulist.PushBack(&utask{
					ListTask: ListTask{
						ID:       lastID,
						MaxRetry: 3,
					},
					uploadInfo: checksum.NewLocalFileInfo(walkedFiles[k3], int(requiredSliceSize)),
					savePath:   path.Clean(absSavePath + "/" + subSavePath),
				})

				fmt.Printf("[%d] 加入上传队列: %s\n", lastID, walkedFiles[k3])
			}
		}
	}

	if lastID == 0 {
		fmt.Printf("未检测到上传的文件, 请检查文件路径或通配符是否正确.\n")
		return
	}

	uploadDatabase, err := pcsupload.NewUploadingDatabase()
	if err != nil {
		fmt.Printf("打开上传未完成数据库错误: %s\n", err)
		return
	}
	defer uploadDatabase.Close()

	var (
		handleTaskErr = func(task *utask, errManifest string, pcsError pcserror.Error) {
			if task == nil {
				panic("task is nil")
			}

			if pcsError == nil {
				return
			}

			// 不重试的情况
			switch pcsError.GetErrType() {
			// 远程服务器错误
			case pcserror.ErrTypeRemoteError:
				switch pcsError.GetRemoteErrCode() {
				case 31200: //[Method:Insert][Error:Insert Request Forbid]
				// do nothing, continue
				default:
					fmt.Printf("[%d] %s, %s\n", task.ID, errManifest, pcsError)
					return
				}
			case pcserror.ErrTypeNetError:
				if strings.Contains(pcsError.GetError().Error(), "413 Request Entity Too Large") {
					fmt.Printf("[%d] %s, %s\n", task.ID, errManifest, pcsError)
					return
				}
			}

			fmt.Printf("[%d] %s, %s, 重试 %d/%d\n", task.ID, errManifest, pcsError, task.retry, task.MaxRetry)

			// 未达到失败重试最大次数, 将任务推送到队列末尾
			if task.retry < task.MaxRetry {
				task.retry++
				ulist.PushBack(task)
				time.Sleep(3 * time.Duration(task.retry) * time.Second)
			} else {
				// on failed
			}
		}
		totalSize int64
	)

	for {
		e := ulist.Front()
		if e == nil { // 结束
			break
		}

		ulist.Remove(e) // 载入任务后, 移除队列

		task := e.Value.(*utask)

		func() {
			fmt.Printf("[%d] 准备上传: %s\n", task.ID, task.uploadInfo.Path)

			if !task.uploadInfo.OpenPath() {
				fmt.Printf("[%d] 文件不可读, 跳过...\n", task.ID)
				return
			}
			defer task.uploadInfo.Close() // 关闭文件

			// 步骤控制
			var (
				err             error
				panDir, panFile = path.Split(task.savePath)
			)

			// 检测断点续传
			state := uploadDatabase.Search(&task.uploadInfo.LocalFileMeta)
			if state != nil || task.uploadInfo.LocalFileMeta.MD5 != nil { // 读取到了md5
				task.step = StepUploadUpload
				goto stepControl
			}

			if opt.NotRapidUpload {
				task.step = StepUploadUpload
				goto stepControl
			}

		stepControl:
			switch task.step {
			case StepUploadRapidUpload:
				goto stepUploadRapidUpload
			case StepUploadUpload:
				goto stepUploadUpload
			}

		stepUploadRapidUpload:
			task.step = StepUploadRapidUpload

			// 设置缓存
			if !pcscache.DirCache.Existed(panDir) {
				fdl, pcsError := pcs.FilesDirectoriesList(panDir, baidupcs.DefaultOrderOptions)
				if pcsError != nil {
					switch pcsError.GetErrType() {
					case pcserror.ErrTypeRemoteError:
						// do nothing
					default:
						fmt.Printf("%s\n", err)
						return
					}
				}
				pcscache.DirCache.Set(panDir, &fdl)
			}

			if task.uploadInfo.Length >= 128*converter.MB {
				fmt.Printf("[%d] 检测秒传中, 请稍候...\n", task.ID)
			}

			task.uploadInfo.Md5Sum()

			// 检测缓存, 通过文件的md5值判断本地文件和网盘文件是否一样
			{
				fd := pcscache.DirCache.FindFileDirectory(panDir, panFile)
				if fd != nil {
					decodedMD5, _ := hex.DecodeString(fd.MD5)
					if bytes.Compare(decodedMD5, task.uploadInfo.MD5) == 0 {
						fmt.Printf("[%d] 目标文件, %s, 已存在, 跳过...\n", task.ID, task.savePath)
						return
					}
				}
			}

			// 文件大于256kb, 应该要检测秒传, 反之则不应检测秒传
			// 经测试, 秒传文件并非一定要大于256KB
			if task.uploadInfo.Length >= requiredSliceSize {
				// do nothing
			}

			// 经过测试, 秒传文件并非需要前256kb切片的md5值, 只需格式符合即可
			task.uploadInfo.SliceMD5Sum()

			// 经测试, 文件的 crc32 值并非秒传文件所必需
			// task.uploadInfo.crc32Sum()

			err = pcs.RapidUpload(task.savePath, hex.EncodeToString(task.uploadInfo.MD5), hex.EncodeToString(task.uploadInfo.SliceMD5), fmt.Sprint(task.uploadInfo.CRC32), task.uploadInfo.Length)
			if err == nil {
				fmt.Printf("[%d] 秒传成功, 保存到网盘路径: %s\n\n", task.ID, task.savePath)
				totalSize += task.uploadInfo.Length
				return
			}

			fmt.Printf("[%d] 秒传失败, 开始上传文件...\n\n", task.ID)

			// 保存秒传信息
			uploadDatabase.UpdateUploading(&task.uploadInfo.LocalFileMeta, nil)
			uploadDatabase.Save()

			// 秒传失败, 开始上传文件
		stepUploadUpload:
			task.step = StepUploadUpload
			{
				muer := uploader.NewMultiUploader(pcsupload.NewPCSUpload(pcs, task.savePath), rio.NewFileReaderAtLen64(task.uploadInfo.File))

				var blockSize int64
				if opt.NotSplitFile {
					blockSize = task.uploadInfo.Length
				} else {
					blockSize = getBlockSize(task.uploadInfo.Length)
				}
				muer.SetBlockSize(blockSize)

				// 设置断点续传
				if state != nil {
					muer.SetInstanceState(state)
				}

				exitChan := make(chan struct{})
				muer.OnExecute(func() {
					statusChan := muer.GetStatusChan()
					updateChan := muer.UpdateInstanceStateChan()
					for {
						select {
						case <-exitChan:
							return
						case v, ok := <-statusChan:
							if !ok {
								return
							}

							if v.TotalSize() == 0 {
								fmt.Printf("\r[%d] Prepareing upload...", task.ID)
								continue
							}

							fmt.Printf("\r[%d] ↑ %s/%s %s/s in %s ............", task.ID,
								converter.ConvertFileSize(v.Uploaded(), 2),
								converter.ConvertFileSize(v.TotalSize(), 2),
								converter.ConvertFileSize(v.SpeedsPerSecond(), 2),
								v.TimeElapsed(),
							)
						case <-updateChan:
							uploadDatabase.UpdateUploading(&task.uploadInfo.LocalFileMeta, muer.InstanceState())
							uploadDatabase.Save()
						}
					}
				})
				muer.OnSuccess(func() {
					close(exitChan)
					fmt.Printf("\n")
					fmt.Printf("[%d] 上传文件成功, 保存到网盘路径: %s\n", task.ID, task.savePath)
					totalSize += task.uploadInfo.Length
					uploadDatabase.Delete(&task.uploadInfo.LocalFileMeta) // 删除
					uploadDatabase.Save()

					// 修复md5
					if !opt.NotFixMD5 && len(task.uploadInfo.MD5) != 0 && task.uploadInfo.Length > blockSize {
						task.retry = 0 // 清空重试次数
						task.uploadedDelayChan = delay.NewDelayChan(10 * time.Second)
						needsFixList.PushBack(task)
					}
				})
				muer.OnError(func(err error) {
					close(exitChan)
					pcsError, ok := err.(pcserror.Error)
					if ok {
						handleTaskErr(task, "上传文件失败", pcsError)
						return
					}
					fmt.Printf("[%d] 上传文件错误: %s\n", task.ID, err)
				})
				muer.Execute()
			}
		}()
	}

	fmt.Printf("\n")
	fmt.Printf("全部上传完毕, 总大小: %s\n", converter.ConvertFileSize(totalSize))

	// 修复上传成功的文件的md5
	// 当文件分片数大于1时, 网盘端最终计算所得的md5值和本地的不一致, 这可能是百度网盘的bug
	// 测试把上传的文件下载到本地后，对比md5值是匹配的
	// 通过秒传的原理来修复md5值
	if !opt.NotFixMD5 && needsFixList.Len() != 0 {
		fmt.Printf("修复上传成功文件的md5中, 共计 %d 个文件...\n", needsFixList.Len())
		for {
			e := needsFixList.Front()
			if e == nil { // 结束
				break
			}

			needsFixList.Remove(e) // 载入任务后, 移除队列

			task := e.Value.(*utask)
			<-task.uploadedDelayChan

			pcsError := pcs.RapidUpload(task.savePath, hex.EncodeToString(task.uploadInfo.MD5), baidupcs.FixSliceMD5(hex.EncodeToString(task.uploadInfo.SliceMD5)), "0", task.uploadInfo.Length)
			if pcsError == nil {
				fmt.Printf("[%d] 修复md5成功, %s\n", task.ID, task.savePath)
				continue
			}

			switch pcsError.GetErrType() {
			// 远程服务器错误
			case pcserror.ErrTypeRemoteError:
				switch pcsError.GetRemoteErrCode() {
				case 31079: //秒传失败
					task.retry++
					if task.retry < task.MaxRetry {
						fmt.Printf("[%d] 修复md5失败, 可能服务器未刷新, 重试 %d/%d\n", task.ID, task.retry, task.MaxRetry)
						needsFixList.PushBack(task)
						time.Sleep(3 * time.Duration(task.retry) * time.Second)
					} else {
						fmt.Printf("[%d] 修复md5失败, %s\n", task.ID, task.savePath)
					}
				default:
					fmt.Printf("[%d] 修复md5失败, 消息: %s\n", task.ID, pcsError)
					continue
				}
			case pcserror.ErrTypeNetError:
				task.retry++
				if task.retry < task.MaxRetry {
					fmt.Printf("[%d] 修复md5失败, %s, 重试 %d/%d\n", task.ID, pcsError, task.retry, task.MaxRetry)
					needsFixList.PushBack(task)
					time.Sleep(3 * time.Duration(task.retry) * time.Second)
				} else {
					fmt.Printf("[%d] 修复md5失败, %s, 消息: %s\n", task.ID, task.savePath, pcsError)
				}
			}
		}
	}
}

func getBlockSize(fileSize int64) int64 {
	blockNum := fileSize / baidupcs.MinUploadBlockSize
	if blockNum > 999 {
		return fileSize/999 + 1
	}
	return baidupcs.MinUploadBlockSize
}
