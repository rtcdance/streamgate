# 恢复的质量判断（P0-3 / C1，2026-10-10）

来源：`git show 0ded20a^:docs/`（commit `0ded20a` 删除的 27 个求职文档中，以下 8 个被恢复）。

| 文件 | 判断 | 说明 |
|------|------|------|
| `P1_MEDIA_WEB3_INTERVIEW_GUIDE.md` | **实质**（保留） | 4 阶段叙事 + 演示流程 + Q&A + 诚实边界，结构完整。⚠️ 文件引用大量过时（`pkg/web3/chain.go`、`pkg/plugins/api/*`、`P0_DEMO_CHECKLIST.md` 均已不存在），C3 重建时必须以当前代码路径修正 |
| `P1_FINAL_INTERVIEW_SCRIPT.md` | **实质**（保留） | 可口述的面试脚本骨架。⚠️ 内嵌测试命令过时（`go test ./cmd/microservices/api-gateway` 等路径已变） |
| `P1_INTERVIEW_SELF_INTRO.md` | **实质**（保留） | 30s/1min/2min 三档自我介绍，实用。⚠️ 同样引用过时路径（`pkg/web3/chain.go` 等） |
| `MOCK_INTERVIEW_QUESTIONS.md` | **实质**（保留） | 模拟问题库 + 「一句话→2-3 点→主动说边界」的排练节奏，可直接使用 |
| `JOB_TARGETING_GUIDE.md` | **实质**（保留） | 岗位匹配分析 + 简历标题纪律（避免 overclaim 清单）。⚠️ 与 `TRANSITION_PLAN.md` 附录 C 的方向优先级不一致（写于该分析之前）：投递顺序以附录 C 为准，本文件用于定位叙述 |
| `RESUME_PROJECT_BLOCK.md` | **实质**（保留） | 按岗位倾向（媒体/Go/Web3）的简历 bullet 变体 + 避免写的内容清单 |
| `CHINESE_RESUME_TEMPLATE.md` | **实质**（保留） | 中文简历模板。⚠️ 简介写「10+ 年 C++」，与 TRANSITION_PLAN C2 的「6 年音视频」口径冲突——**以用户真实年数为准，C0/C2 产出时确认** |
| `ENGLISH_RESUME_TEMPLATE.md` | **实质**（保留） | 英文模板，同上口径问题 |

**结论**：8/8 有实质内容，无 AI 填充物，全部保留。所有文件的历史引用以恢复时点为准；凡用于面试/简历的内容，以 `TRANSITION_PLAN.md` 附录 A.3（已核实的 file:line 优势）与当前代码为准。
