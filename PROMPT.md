我们需要为控制平面实现一个并发安全的内存前缀所有权注册表，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `prefixclaim` 包是可编译的公开接口骨架，请读取项目中的 `SPEC.md` 并完成实现。

注册表支持 Claim、Release 原子批次。Claim 与已有相同路径、祖先路径或后代路径均冲突；Release 必须精确匹配路径和 owner。批次必须先完成全部结构校验，再在隔离候选状态中按输入顺序执行；释放后再声明、根路径、段边界和最终容量都要正确处理。冲突、缺失、owner 不匹配或容量失败必须整体回滚且不能消耗 revision。还需实现最近祖先 Lookup、稳定 Descendants 游标分页和 Snapshot。

请保留公开 API 以及 `SPEC.md` 规定的校验顺序、前缀段语义、批次顺序、回滚、revision、generation、最终容量、排序、分页、返回结构和错误语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`prefixclaim/contract_test.go`、`cmd/demo/main.go`；可以修改接口骨架并新增测试。不要增加第三方依赖、访问网络、删除或弱化测试、硬编码结果，也不要创建 Git 提交。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果。
