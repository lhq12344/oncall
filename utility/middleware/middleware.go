package middleware

import (
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"
)

// CORSMiddleware 处理CORS跨域请求
func CORSMiddleware(r *ghttp.Request) {
	r.Response.CORSDefault()
	r.Middleware.Next()
}

func ResponseMiddleware(r *ghttp.Request) {
	r.Middleware.Next()

	// 对 SSE 或已直接写出的响应不再二次包装，避免破坏流式协议。
	contentType := strings.ToLower(r.Response.Header().Get("Content-Type"))
	if strings.Contains(contentType, "text/event-stream") {
		return
	}
	if r.Response.BytesWritten() > 0 || r.Response.BufferLength() > 0 {
		return
	}

	var (
		msg string
		res = r.GetHandlerResponse()
		err = r.GetError()
	)
	if err != nil {
		msg = err.Error()
	} else {
		msg = "OK"
	}
	r.Response.WriteJson(Response{
		Message: msg,
		Data:    res,
	})
}

type Response struct {
	Message string      `json:"message" dc:"消息提示"`
	Data    interface{} `json:"data"    dc:"执行结果"`
}
