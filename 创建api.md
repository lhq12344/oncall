

# 创建模板 API

**请求方法**: `POST /msg/create_template`

 请求字段

| 字段名     | 类型    | 描述                                       |
|------------|---------|--------------------------------------------|
| name       | string  | 模板名字                                   |
| content    | string  | 模板内容，支持占位符                       |
| channel    | int     | 推送渠道，1：邮件，2：短信                 |
| sourceID   | string  | 哪个渠道创建的这个模板                     |
| subject    | string  | 消息主题                                   |
| signName   | string  | 签名名字，少部分场景有用，比如阿里云短信 |

返回字段

| 字段名     | 类型    | 描述                                       |
|------------|---------|--------------------------------------------|
| code       | int     | 返回编码，0表示成功                        |
| msg        | string  | 返回信息                                   |
| templateID | string  | 模板ID                                     |
