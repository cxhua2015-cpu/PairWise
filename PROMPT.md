我们需要为分布式控制面实现一个并发安全的内存型“到期状态表 429”，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `expirytable429` 包是可编译公开接口骨架，请读取 `SPEC.md` 并完成实现。

核心语义：表使用显式非负单调时间。Apply 先结构校验再检查时间，在候选状态先删除 ExpiresAt <= Now 的条目，再顺序执行 Put/Touch/Delete；Put/Touch 分配 revision。最终容量失败或任何错误须连同淘汰、时间和 revision 一起回滚。Expire 使用相同闭区间边界。 所有公开方法必须支持并发调用。

请保留公开 API、错误值和规范顺序。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`expirytable429/contract_test.go`、`cmd/demo/main.go`；不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。请补充边界与并发测试，在 README 中说明索引、候选事务、所有权及复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`。
