我们需要为控制平面实现一个并发安全、使用显式时间的内存延迟优先队列，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `delayedqueue` 包是可编译的公开接口骨架，请读取项目中的 `SPEC.md` 并完成实现。

队列支持 Enqueue、Reschedule、Cancel 原子批次。批次必须先完成全部结构校验，再检查全局时间单调性，并在隔离候选状态中按输入顺序执行；Enqueue 和 Reschedule 分配连续 revision，Cancel 不分配。ID 冲突、任务缺失、时间倒退、最终任务数或 Payload 总字节容量失败必须整体回滚且不能消耗 revision。同批取消后重新入队、重复调度和临时超量但最终合规都要正确处理。还需实现按 Priority 降序、ReadyAt 升序、ID 升序稳定选择的 Peek 和 Take，以及稳定 Snapshot；所有 Payload 输入输出必须深拷贝。

请保留公开 API 以及 `SPEC.md` 规定的校验顺序、时间、批次顺序、回滚、revision、generation、最终容量、优先级排序、返回结构和错误语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`delayedqueue/contract_test.go`、`cmd/demo/main.go`；可以修改接口骨架并新增实现文件和测试。不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。

请补充时间边界、顺序批次、revision 回滚、最终容量、Payload 所有权、稳定排序和并发测试，并在 `README.md` 中说明索引、候选事务、时间、排序、容量、所有权以及实际复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果与未完成内容。
