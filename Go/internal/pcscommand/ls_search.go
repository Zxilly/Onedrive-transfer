package pcscommand

import (
	"fmt"
	"github.com/iikira/BaiduPCS-Go/baidupcs"
	"github.com/iikira/BaiduPCS-Go/pcstable"
	"github.com/iikira/BaiduPCS-Go/pcsutil/converter"
	"github.com/iikira/BaiduPCS-Go/pcsutil/pcstime"
	"github.com/olekukonko/tablewriter"
	"os"
	"strconv"
)

type (
	// LsOptions 列目录可选项
	LsOptions struct {
		Total bool
	}

	SearchOptions struct {
		Total   bool
		Recurse bool
	}
)

const (
	opLs int = iota
	opSearch
)

// RunLs 执行列目录
func RunLs(path string, lsOptions *LsOptions, orderOptions *baidupcs.OrderOptions) {
	path, err := getAbsPath(path)
	if err != nil {
		fmt.Println(err)
		return
	}

	files, err := GetBaiduPCS().FilesDirectoriesList(path, orderOptions)
	if err != nil {
		fmt.Println(err)
		return
	}

	if lsOptions == nil {
		lsOptions = &LsOptions{}
	}

	renderTable(opLs, lsOptions.Total, path, files)
	return
}


func renderTable(op int, isTotal bool, path string, files baidupcs.FileDirectoryList) {
	tb := pcstable.NewTable(os.Stdout)
	var (
		fN, dN int64
	)

	if isTotal {
		tb.SetHeader([]string{"#", "fs_id", "文件大小", "创建日期", "修改日期", "md5(截图请打码)", "文件(目录)"})
		tb.SetColumnAlignment([]int{tablewriter.ALIGN_DEFAULT, tablewriter.ALIGN_RIGHT, tablewriter.ALIGN_RIGHT, tablewriter.ALIGN_LEFT, tablewriter.ALIGN_LEFT, tablewriter.ALIGN_LEFT, tablewriter.ALIGN_LEFT})
		for k, file := range files {
			if file.Isdir {
				tb.Append([]string{strconv.Itoa(k), strconv.FormatInt(file.FsID, 10), "-", pcstime.FormatTime(file.Ctime), pcstime.FormatTime(file.Mtime), file.MD5, file.Filename + "/"})
				continue
			}

			var md5 string
			if len(file.BlockList) > 1 {
				md5 = "(可能不正确)" + file.MD5
			} else {
				md5 = file.MD5
			}

			switch op {
			case opLs:
				tb.Append([]string{strconv.Itoa(k), strconv.FormatInt(file.FsID, 10), converter.ConvertFileSize(file.Size, 2), pcstime.FormatTime(file.Ctime), pcstime.FormatTime(file.Mtime), md5, file.Filename})
			case opSearch:
				tb.Append([]string{strconv.Itoa(k), strconv.FormatInt(file.FsID, 10), converter.ConvertFileSize(file.Size, 2), pcstime.FormatTime(file.Ctime), pcstime.FormatTime(file.Mtime), md5, file.Path})
			}
		}
		fN, dN = files.Count()
		tb.Append([]string{"", "", "总: " + converter.ConvertFileSize(files.TotalSize(), 2), "", "", "", fmt.Sprintf("文件总数: %d, 目录总数: %d", fN, dN)})
	} else {
		for k, file := range files {
			if file.Isdir {
				tb.Append([]string{strconv.Itoa(k), "-", pcstime.FormatTime(file.Mtime), file.Filename + "/"})
				continue
			}

			switch op {
			case opLs:
				tb.Append([]string{file.Filename})
			case opSearch:
				tb.Append([]string{strconv.Itoa(k), converter.ConvertFileSize(file.Size, 2), pcstime.FormatTime(file.Mtime), file.Path})
			}


		}


	}

	tb.Render()

	if fN+dN >= 50 {
		fmt.Printf("\n当前目录: %s\n", path)
	}
}