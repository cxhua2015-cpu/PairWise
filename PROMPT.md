我们需要为支付入口实现一个并发安全的内存幂等请求注册表，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `idempotency` 包是可编译的公开接口骨架，请读取项目中的 `SPEC.md` 并完成实现。

注册表需要按请求 key 和 fingerprint 协调执行权：首次请求获得带 fencing token 的 leader 租约；相同 fingerprint 的并发请求观察 pending，成功完成后可 replay 深拷贝结果；不同 fingerprint 必须冲突。需要支持续租、完成、放弃、按明确时间执行过期清扫及稳定快照。过期 pending 可以被新 leader 接管，旧 token 此后必须失效；完成记录在 replay TTL 到期后才可被清扫。容量只统计当前记录以及保存的结果字节，所有失败操作不得改变状态、计数或 generation，成功的可观察状态变更每次只将 generation 推进一次。

请保留公开 API 以及 `SPEC.md` 规定的校验顺序、时间边界、token 分配、状态转换、容量、排序、返回结构和错误语义。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`idempotency/contract_test.go`、`cmd/demo/main.go`；可以修改接口骨架并新增实现文件和测试。不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。

请补充接管与陈旧 token、时间/容量边界、所有权和并发测试，并在 `README.md` 中说明状态机、fencing、过期、容量、所有权以及实际时间和空间复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果与未完成内容。
