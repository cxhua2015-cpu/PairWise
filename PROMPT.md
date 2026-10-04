我们需要为控制平面实现一个并发安全、使用显式时间的内存断路器注册表，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `breaker` 包是可编译的公开接口骨架，请读取项目中的 `SPEC.md` 并完成实现。

每个服务按策略在 Closed、Open、HalfOpen 三态间转换，支持原子批量 Record。批次必须先完成全部结构校验，再检查全局时间单调性，并在隔离候选状态中先把到期 Open 服务推进为 HalfOpen、再按输入顺序处理成功/失败事件。未知服务、仍处于 Open 的事件、时间倒退或任何失败都必须整体回滚，批次开始时的自动转换、时间、计数器和 generation 也不能泄漏。OpenUntil 加法必须在 int64 极值处饱和。另需实现 Allow、Sweep 和稳定 Snapshot。

请保留公开 API 以及 `SPEC.md` 规定的校验顺序、状态机、时间边界、批次顺序、回滚、generation、排序、返回结构和错误语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`breaker/contract_test.go`、`cmd/demo/main.go`；可以修改接口骨架并新增实现文件和测试。不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。

请补充批次回滚、状态边界、同批多次转换、极值时间、Allow/Sweep、跨服务隔离、排序和并发测试，并在 `README.md` 中说明索引、状态机、事务、时间与溢出处理以及实际复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果与未完成内容。
