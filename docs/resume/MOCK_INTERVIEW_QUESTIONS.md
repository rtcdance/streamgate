# StreamGate Mock Interview Questions

## Goal

Use these questions to rehearse the project in a realistic interview style.

## Project positioning

1. 你为什么要做这个项目，而不是做一个更通用的 Go 后端项目？
2. 这个项目最想证明你具备什么能力？
3. 为什么这个项目适合你当前的转型方向？
4. 这个项目和纯 Web3 demo 最大的区别是什么？

## Architecture

1. 你为什么把 Web3 放在授权层，而不是做成系统的核心主线？
2. 为什么你先做单体 / gateway 路线，再考虑微服务拆分？
3. 你觉得这个项目当前最核心的业务闭环是什么？
4. 如果继续往生产环境推进，你会优先补哪一层？

## Wallet auth

1. 为什么不能只让用户传一个钱包地址就算登录？
2. challenge 登录的核心安全价值是什么？
3. 如何避免 replay attack？
4. 如果要做多实例部署，challenge 状态应该放在哪里？

## NFT authorization

1. 你怎么验证 NFT 持有关系？
2. 为什么同时支持 `balanceOf` 和 `ownerOf`？
3. 为什么你说 ownership 是真实的，但 metadata 还不算完整？
4. `cache_hit` 的意义是什么？

## Streaming path

1. 为什么你把鉴权放在 manifest，而不是每个 segment？
2. playback token 的作用是什么？
3. 这样做对延迟和链上依赖有什么影响？
4. 如果以后接 CDN，你会怎么处理缓存边界？

## Transcoding / worker

1. 你为什么要把转码和 worker 也纳入这个项目？
2. 现在的转码链已经具备哪些能力？
3. worker 调度里你最看重的工程点是什么？
4. 为什么优先做任务提交、状态查询、取消、重试这些能力？

## Testing

1. 你是怎么验证这个项目不是“只看起来能跑”的？
2. 目前测试最完整覆盖的是哪些路径？
3. 哪些测试是 mock 驱动，哪些更接近真实链路？
4. 你为什么要补 service、api、gateway、plugin 多层测试？

## RPC high availability

1. 你为什么要补多 RPC failover？
2. 现在的 failover 已经做到什么程度？
3. active RPC、failure cooldown 这些状态为什么重要？
4. 如果继续做生产级高可用，你下一步会补什么？

## Honest boundaries

1. 这个项目现在还没有完成哪些部分？
2. 如果面试官问“这是不是生产级系统”，你会怎么回答？
3. 哪些点是你故意没有做满的？
4. 为什么你觉得当前这个完成度已经足够支持投递？

## Best closing questions to practice

1. 如果让你再做两周，你会继续补什么？
2. 如果让你把这个项目讲成一句话，你会怎么说？
3. 如果让你把这个项目写进简历，你会强调哪三点？
4. 如果让你选择目标岗位顺序，你会怎么排？

## Recommended rehearsal style

Do not try to answer every question with maximum detail.

Use this rhythm:

1. 先一句话回答
2. 再补 2-3 个关键点
3. 最后主动说清边界

That will sound more natural than a long monologue.
