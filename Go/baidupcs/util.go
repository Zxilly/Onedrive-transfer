package baidupcs

import (
	"errors"
	"github.com/iikira/BaiduPCS-Go/baidupcs/pcserror"
	"path"
	"strings"
)

// Isdir 检查路径在网盘中是否为目录
func (pcs *BaiduPCS) Isdir(pcspath string) (isdir bool, pcsError pcserror.Error) {
	if path.Clean(pcspath) == "/" {
		return true, nil
	}

	f, pcsError := pcs.FilesDirectoriesMeta(pcspath)
	if pcsError != nil {
		return false, pcsError
	}

	return f.Isdir, nil
}

func (pcs *BaiduPCS) checkIsdir(op string, targetPath string) pcserror.Error {
	// 检测文件是否存在于网盘路径
	// 很重要, 如果文件存在会直接覆盖!!! 即使是根目录!
	isdir, pcsError := pcs.Isdir(targetPath)
	if pcsError != nil {
		// 忽略远程服务端返回的错误
		if pcsError.GetErrType() != pcserror.ErrTypeRemoteError {
			return pcsError
		}
	}

	errInfo := pcserror.NewPCSErrorInfo(op)
	if isdir {
		errInfo.ErrType = pcserror.ErrTypeOthers
		errInfo.Err = errors.New("保存路径不可以覆盖目录")
		return errInfo
	}
	return nil
}

func mergeStringList(a ...string) string {
	s := strings.Join(a, `","`)
	return `["` + s + `"]`
}

// GetHTTPScheme 获取 http 协议, https 或 http
func GetHTTPScheme(https bool) (scheme string) {
	if https {
		return "https"
	}
	return "http"
}

// FixSliceMD5 修复slicemd5为合法的md5
func FixSliceMD5(slicemd5 string) string {
	if len(slicemd5) != 32 {
		return DefaultSliceMD5
	}
	return slicemd5
}
