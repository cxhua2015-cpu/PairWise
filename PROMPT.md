我们需要为资源调度器实现一个并发安全、显式时间驱动的内存租约池，使用 Go 1.22 或更高版本且仅依赖标准库。当前 `leasepool` 包是可编译的公开接口骨架，请读取 `SPEC.md` 并完成实现。

租约池支持 Add、Remove、Acquire、Renew、Release 原子批次，以及 Expire、Snapshot。批次必须先完成全部结构校验，再检查时间单调性，并在隔离候选状态中按输入顺序执行。失败要整体回滚，不能推进时间、generation 或 revision。过期租约在 Acquire 时视为空闲但只有成功提交才能移除；最终资源数容量仅在批次末检查。所有返回切片必须隔离，所有公开方法必须支持并发调用。

请保留公开 API、错误值及 `SPEC.md` 规定的校验顺序、边界、批次顺序、revision/generation、过期和所有权语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`leasepool/contract_test.go`、`cmd/demo/main.go`；不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。

请补充必要的边界和并发测试，在 `README.md` 中说明索引、候选事务、过期规则、revision/generation 与时间/空间复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果与未完成内容。
