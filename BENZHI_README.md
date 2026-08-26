# BENZHI_README

## 项目说明
- 项目：benzhi-project-8d70dd20-9a9e-49dc-9839-ed057c041bbf
- 项目用途：已完整实现濒危语言田野录音从建档、授权核验、转写标注、隐私脱敏、独立伦理复核到发布封存的浏览器工作流，并提供原子本地持久化、可验证审计链、确定性发布清单及真实回环 HTTP 自检。
- Go 工具链：`golang:1.22`
- 前端工具链：原生 HTML、CSS 和 JavaScript，由 Go 服务直接提供

## 项目描述
- 项目名称：field-voice-archive
- 项目介绍：为濒危语言田野录音建立从知情授权、内容标注、隐私匿名化到开放档案发布的可追溯审核流程，研究人员在浏览器页面中逐步推进一个录音批次并在最终发布后只读封存。
- 项目概述：为濒危语言田野录音建立从知情授权、内容标注、隐私匿名化到开放档案发布的可追溯审核流程，研究人员在浏览器页面中逐步推进一个录音批次并在最终发布后只读封存。
- 核心工作流：录音批次建档后校验参与者授权，导入媒体校验摘要，完成转写与说话人标注，执行隐私脱敏，经过独立伦理复核后发布不可变开放档案。
- 对外接口：Go 服务提供浏览器 web_ui：在 / 页面完成批次创建、授权材料上传、标注编辑、脱敏预览、复核签名和发布查询；同时提供同一版本的 JSON API 供页面调用。监听地址支持 -addr=127.0.0.1:<port> 或 PORT 环境变量（端口号绑定 127.0.0.1:<PORT>），默认 127.0.0.1:19081；根目录 README.md 说明标准构建、运行和测试方式。

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...

cd /app && GOTOOLCHAIN=local go run ./cmd/fieldarchive --self-check -addr=127.0.0.1:19081

cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh

./build_benzhi_docker.sh benzhi-project-8d70dd20-9a9e-49dc-9839-ed057c041bbf-amd64 linux/amd64

./build_benzhi_docker.sh benzhi-project-8d70dd20-9a9e-49dc-9839-ed057c041bbf-arm64 linux/arm64

docker run -it benzhi-project-8d70dd20-9a9e-49dc-9839-ed057c041bbf-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run ./cmd/fieldarchive --self-check -addr=127.0.0.1:19081`
