# field-voice-archive

`field-voice-archive` 为濒危语言田野录音提供可追溯的开放档案工作流。调查员可在浏览器中创建录音批次、登记知情授权、导入时间码与转写、标记个人信息并预览脱敏结果；独立伦理复核人签署意见后，由独立档案管理员发布确定性清单。发布批次随即只读封存，状态、脱敏文本、审计事件链和证据清单仍可查询。

系统只依赖 Go 标准库。数据保存在本地原子 JSON 快照、幂等响应索引和带前序 SHA-256 的追加式 JSONL 审计链中。启动时会校验快照和审计链，损坏快照会移入数据目录的 `quarantine` 子目录。

## 构建

```sh
go build ./cmd/fieldarchive
```

## 运行

```sh
go run ./cmd/fieldarchive -addr=127.0.0.1:19081 -data-dir=./data
```

然后访问 `http://127.0.0.1:19081/`。默认监听地址为 `127.0.0.1:19081`，也可设置 `PORT` 端口号，此时服务绑定 `127.0.0.1:<PORT>`。为避免意外暴露田野资料，程序拒绝非回环监听地址。

## 测试与自检

运行全部自动化测试：

```sh
go test ./...
```

真实 HTTP 自检会使用临时数据目录走完建档、授权、标注、脱敏、复核、发布和只读封存流程，结束后自行退出：

```sh
go run ./cmd/fieldarchive --self-check -addr=127.0.0.1:19081
```

JSON API 位于 `/api/v1`。所有写命令都需要 `actor`、唯一 `request_id`，除创建外还需要当前 `expected_revision`；相同 `request_id` 与相同载荷会返回既有结果，不同载荷则被拒绝。

批次详情支持授权撤回（`POST /api/v1/batches/{batchID}/consents/withdraw`）、授权批量导入、转写 `dry_run` 预检、脱敏预览与修订项关闭。发布采用 `release` 预检返回的一次性 `lock_token` 和 `expected_manifest_digest` 签发；已发布批次可通过 `POST /api/v1/batches/verify` 批量核验清单、媒体摘要和审计链。
