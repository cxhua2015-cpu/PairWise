我们需要为服务配置系统实现一个并发安全的内存分层配置栈，使用 Go 1.22 或更高版本且仅依赖标准库。当前 `configstack` 包是可编译接口骨架，请读取 `SPEC.md` 并完成实现。

配置栈支持原子批量 AddLayer、RemoveLayer、Set、Delete 和 Move。层优先级由顺序决定，Resolve 返回最高优先级命中。批次先做全部结构校验，再在隔离候选状态按输入顺序执行，并只检查最终层数、条目数和字节容量。失败整体回滚，不消耗 revision/generation。输入输出字节切片必须深拷贝，公开方法支持并发调用。

请保留公开 API 和错误值，不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`configstack/contract_test.go`、`cmd/demo/main.go`。不要增加依赖、访问网络、弱化测试或创建 Git 提交。请补充边界/并发测试，在 README 说明索引、顺序、事务、所有权和复杂度，并执行三项验证命令。
