我们需要为配置中心实现一个并发安全的内存 JSON Patch 文档存储，使用 Go 1.22 或更高版本，仅依赖标准库。当前 `jsondoc` 包是可编译的公开接口骨架，请读取项目中的 `SPEC.md` 并完成实现。

存储需要严格处理 JSON Pointer 转义、对象成员、数组下标与追加，并支持 add/remove/replace/move/copy/test 六类操作。每批补丁必须先完整校验，再基于期望 revision 在隔离副本中按顺序执行；任何失败都要回滚，只有含变更操作的成功批次才推进 revision。实现还需阻止 move 到自身后代，按 JSON 数值语义执行 test，只在批次最终状态检查节点预算，并保证输入、结果和快照的所有权隔离。所有公开方法需要支持并发调用。

请保留公开 API 以及 `SPEC.md` 规定的校验顺序、JSON Pointer 边界、数组语义、返回结构和错误包装。不得修改 `SPEC.md`、`PROMPT.md`、`go.mod`、`jsondoc/contract_test.go`、`cmd/demo/main.go`；可以修改接口骨架实现并新增实现文件和测试。不要增加第三方依赖、访问网络、删除或弱化测试、硬编码示例结果，也不要创建 Git 提交。

请补充你认为必要的边界和并发测试，并在 `README.md` 中说明内部 JSON 表示、Pointer 解析、六类操作、批量原子性、revision、节点计数、数值比较、Payload 所有权以及实际时间和空间复杂度。完成后执行 `go test ./...`、`go test -race ./...` 和 `go run ./cmd/demo`，如实报告结果与未完成内容。
