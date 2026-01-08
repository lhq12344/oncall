package loader

import (
	"context"

	"github.com/cloudwego/eino-ext/components/document/loader/file"
	"github.com/cloudwego/eino/components/document"
)

// NewFileLoader 封装创建一个“文件类 Loader（加载器）”的流程，统一配置与错误处理，对外返回一个实现了 document.Loader 接口的实例
func NewFileLoader(ctx context.Context) (ldr document.Loader, err error) {
	config := &file.FileLoaderConfig{}
	ldr, err = file.NewFileLoader(ctx, config)
	if err != nil {
		return nil, err
	}
	return ldr, nil
}
