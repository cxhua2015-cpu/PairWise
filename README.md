# resourceledger102

并发安全的内存型资源计量账本。语义见 `SPEC.md`，公开契约见
`resourceledger102/contract_test.go`。

## 设计说明

### 索引
- 主索引为 `map[string]Account`（按名称寻址，O(1) 读写）。
- 不维护持久有序索引；`Top` 与 `Snapshot` 在调用时对当前账户做一次性
  排序拷贝（`sort.Slice`），避免写路径为有序结构付出额外代价。

### 候选事务（candidate transaction）
`Apply` 分两阶段执行：
1. **结构校验**：先完整校验所有 op 的 kind 与名称合法性，不读取任何状态；
   未知 kind / 非法名称直接返回 `ErrInvalidInput`。
2. **暂存执行**：所有写入先落在候选覆盖层（`cand` map，键为账户名，值为
   暂存账户或删除标记），读操作先查覆盖层再回落到主索引。int64 溢出在
   算术前检测，绝对值上限在每次 Add/Set 后立即执行；账户容量上限
   （`MaxAccounts`）仅在批次末对最终账户数检查一次。任一检查失败即丢弃
   整个候选层，主索引、revision、generation 完全不变（整体回滚）；全部
   通过后才把覆盖层合并进主索引，revision 连续分配，非空批次 generation
   恰好加一。

### 所有权与并发
- 全部公开方法由同一把 `sync.Mutex` 保护，可任意并发调用。
- `Account` 为纯值类型；`Top`/`Snapshot`/`Result.Changed` 返回的切片均为
  新建拷贝，调用方对返回值的修改不会影响账本内部状态。
- `Ledger` 实例所有权归调用方，内部状态绝不逃逸到返回值中。

### 复杂度
- `Apply`：O(k)，k 为批次内 op 数（另加 O(k) 候选层空间）。
- `Top`：O(n log n)，n 为当前账户数。
- `Snapshot`：O(n log n)（按名称排序）。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
