# Rokid Hermes Connector

把 Rokid Glasses / 灵珠平台接入 Home Assistant Conversation 或 Hermes/Rhasspy MQTT 生态。

如果你已经在使用 Rhasspy、Hermes、MQTT 或 Home Assistant Conversation，这个连接件可以把 Rokid 眼镜侧的文本输入转换为 HA Conversation 请求或 Hermes intent payload。

## 功能特性

- **灵珠平台 SSE 入口**：提供 `POST /rokid/sse`。
- **HA Direct 模式**：直接调用 Home Assistant `/api/conversation/process`。
- **Hermes Log 模式**：生成 Hermes intent payload 并写入日志，便于调试。
- **Hermes MQTT 模式**：将 intent payload 发布到 MQTT broker。
- **安全鉴权**：支持 `Authorization: Bearer <ak>` 与 `X-Auth-AK`。
- **轻量部署**：支持本地运行、Docker 和 docker-compose。

## 模式

| MODE | 状态 | 说明 |
| --- | --- | --- |
| `ha-direct` | 可用 | 直接调用 Home Assistant `/api/conversation/process` |
| `hermes-log` | 可用 | 生成 Hermes intent payload 并写入日志，适合无 MQTT 依赖调试 |
| `hermes-mqtt` | 可用 | 通过 MQTT 发布 Hermes intent payload |

## HTTP 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/health` | 健康检查 |
| `POST` | `/rokid/sse` | 灵珠平台 SSE 入口 |

`/rokid/sse` 接收灵珠平台常见 JSON 字段：`text`、`content`、`query`、`input`、`sessionId`。

## 快速开始

```bash
cp .env.example .env
```

编辑 `.env`：

```env
PORT=8081
MODE=ha-direct
HA_URL=http://homeassistant.local:8123
HA_TOKEN=replace-with-home-assistant-token
LANGUAGE=zh-cn
ROKID_AUTH_AK=replace-with-rokid-auth-ak
HERMES_SITE_ID=rokid-glasses
HERMES_TOPIC=hermes/intent/RokidCommand
MQTT_HOST=127.0.0.1
MQTT_PORT=1883
MQTT_USERNAME=
MQTT_PASSWORD=
```

运行：

```bash
go run .
```

健康检查：

```bash
curl http://127.0.0.1:8081/health
```

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8081` | HTTP 服务端口 |
| `MODE` | `ha-direct` | `ha-direct`、`hermes-log` 或 `hermes-mqtt` |
| `HA_URL` | 空 | Home Assistant 地址 |
| `HA_TOKEN` | 空 | Home Assistant Long-Lived Access Token |
| `LANGUAGE` | `zh-cn` | HA Conversation 语言 |
| `ROKID_AUTH_AK` | 空 | 灵珠平台 Auth AK；为空时仅允许本机测试 |
| `HERMES_SITE_ID` | `rokid-glasses` | Hermes siteId |
| `HERMES_TOPIC` | `hermes/intent/RokidCommand` | Hermes intent topic |
| `MQTT_HOST` | 空 | MQTT broker 主机或完整 broker URL |
| `MQTT_PORT` | `1883` | MQTT broker 端口 |
| `MQTT_USERNAME` | 空 | MQTT 用户名 |
| `MQTT_PASSWORD` | 空 | MQTT 密码 |

## Hermes payload

`hermes-mqtt` 会发布类似下面的 payload：

```json
{
  "input": "打开客厅灯",
  "siteId": "rokid-glasses",
  "sessionId": "debug",
  "intent": {
    "intentName": "RokidCommand"
  },
  "slots": []
}
```

默认 topic：`hermes/intent/RokidCommand`。

## Docker

```bash
docker build -t rokid-hermes-connector .
docker run --rm -p 8081:8081 \
  -e MODE="ha-direct" \
  -e HA_URL="http://homeassistant.local:8123" \
  -e HA_TOKEN="replace-with-home-assistant-token" \
  -e ROKID_AUTH_AK="replace-with-rokid-auth-ak" \
  rokid-hermes-connector
```

## 请求示例

```bash
curl -N -X POST http://127.0.0.1:8081/rokid/sse \
  -H 'Authorization: Bearer replace-with-rokid-auth-ak' \
  -H 'Content-Type: application/json' \
  -d '{"text":"打开客厅灯","sessionId":"debug"}'
```

## 灵珠平台配置

- SSE URL：`https://<your-domain>/rokid/sse`
- Auth AK：与 `ROKID_AUTH_AK` 一致
- 输入类型：Text
- 服务端兼容 `text`、`content`、`query`、`input` 和 `sessionId` 字段

生产环境建议使用 HTTPS 反向代理，并只暴露 `/rokid/sse`。

## Home Assistant Token 创建

1. 在 Home Assistant 中打开用户头像菜单。
2. 找到 Long-Lived Access Tokens。
3. 创建专用于 Hermes Connector 的 token。
4. 将 token 写入 `HA_TOKEN`，不要提交到仓库。

## 反向代理示例

Caddy：

```caddyfile
rokid-hermes.example.com {
  reverse_proxy /rokid/sse 127.0.0.1:8081
}
```

Nginx：

```nginx
location /rokid/sse {
  proxy_pass http://127.0.0.1:8081;
  proxy_http_version 1.1;
  proxy_set_header Host $host;
  proxy_set_header Connection "";
  proxy_buffering off;
}
```

## 故障排查

| 现象 | 检查项 |
| --- | --- |
| `/rokid/sse` 返回 unauthorized | 检查 `ROKID_AUTH_AK` 和请求头是否一致 |
| `ha-direct` 调用失败 | 检查 `HA_URL`、`HA_TOKEN` 和 Home Assistant Conversation 是否可用 |
| `hermes-mqtt` 无法发布 | 检查 `MQTT_HOST`、`MQTT_PORT`、用户名、密码和 broker 网络连通性 |
| Rhasspy/Hermes 没收到 intent | 检查 `HERMES_TOPIC` 是否与订阅 topic 一致 |
| SSE 没有持续输出 | 检查反向代理是否关闭 buffering，并用 `curl -N` 测试 |

## 开发

```bash
go fmt ./...
go test ./...
go build ./...
```

## 许可证

MIT。
