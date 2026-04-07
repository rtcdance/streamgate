# StreamGate 中文简历项目段落

## 推荐项目标题

`StreamGate：基于 Go 的 NFT-gated Streaming Backend`

## 推荐项目描述

设计并实现一个基于 Go 的受保护媒体访问后端系统，将钱包 challenge 登录、NFT 持有校验、HLS manifest 鉴权、短时 playback token、转码任务接口与 worker 调度结合起来，构建了一个更贴近真实业务场景的内容访问控制系统。

## 推荐项目要点

- 设计并实现钱包 challenge 登录、签名验签、JWT 签发和 NFT 持有校验链路，将 Web3 身份与授权能力接入受保护内容访问场景。
- 设计 HLS 鉴权方案：在 manifest 请求阶段完成 NFT 校验，后续 segment 请求通过短时 playback token 放行，避免在热路径上重复访问链上 RPC。
- 实现转码任务提交、状态查询、任务列表、取消和 profile 查询接口，并补齐 worker 调度、重试、取消、健康检查等媒体后台基础能力。
- 为认证、NFT 校验、流媒体访问、转码任务、worker/transcoder 路径补充自动化测试，覆盖 replay protection、cache-hit、受保护播放、任务生命周期等关键行为。

## 简洁版

实现一个基于 Go 的 NFT-gated Streaming Backend，将钱包登录、NFT 授权、受保护 HLS 播放、转码任务接口和 worker 调度整合进同一个媒体访问控制系统。

## 更适合媒体后端岗位的写法

- 设计并实现受保护流媒体访问后端，包含 manifest 鉴权、playback token、转码任务接口和 worker 调度能力。
- 将 Web3 用作授权层，通过钱包 challenge 登录和 NFT 持有校验实现内容访问控制，而不是将项目做成纯链上 demo。

## 更适合 Go 后端岗位的写法

- 基于 Go 实现认证、授权、任务处理、状态流转和测试验证链路，覆盖 auth、streaming、gateway、worker、transcoder 多层模块。
- 在单体与微服务入口之间保持接口语义一致，便于后续架构演进与面试展示。

## 使用建议

默认优先使用这版投递：

- 媒体后端
- 流媒体后端
- Go 后端

如果岗位更偏 Web3 后端，再把“钱包登录 / NFT 授权”两条 bullet 往前提。
