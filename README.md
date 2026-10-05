# resourceledger122

并发安全的内存型资源计量账本（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

- **索引**：核心状态为 `map[string]Account`（按名称 O(1) 查找），辅以单调递增的
  `generation` 与 `revision` 计数器。`Top`/`Snapshot` 在读取时拷贝并排序，不维护
  额外的有序索引，以换取写入路径的 O(1) 摊还复杂度。
- **候选事务（staging）**：`Apply` 先在无锁状态下对整个批次做纯结构校验（kind、
  名称字符集与长度、多余字段），再加写锁，把批次涉及账户的候选状态暂存于局部
  `pending` 映射中按输入顺序执行。Add/Set 在暂存区分配连续 revision；溢出与绝对值
  上限在算术前/后立即检测；最终账户容量只在批次末尾对暂存结果检查一次。任一失败
  直接返回，账本状态零改动（整体回滚）；全部通过才一次性提交并使 generation 增一。
- **所有权**：所有公开方法由一把 `sync.RWMutex` 保护（写操作独占，Top/Snapshot 共享
  读）。返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建拷贝，调用方
  修改不会影响内部状态；`Ledger` 不可复制，应通过指针共享。
- **复杂度**：设批次含 k 个 op、账本含 n 个账户。`Apply` 为 O(k)（校验 + 暂存 + 提交）；
  `Top` 为 O(n log n)；`Snapshot` 为 O(n log n)；空间 O(n + k)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
