# balanceledger437

并发安全的内存型余额账本（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Add/Set/Delete，Add/Set 分配连续 revision，失败整体回滚。

## 架构与文件

- `creditpool.go` — 核心事务引擎：`Ledger`（`sync.RWMutex` + 不可变提交的状态）、
  `Apply`/`Top`/`Snapshot` 及共享的 `applyBatch`。
- `validation.go` — 无副作用批次预检 `ValidateBatch`：仅做结构校验（kind、名称
  字符集与字节上限、Add 的 Delta 非零），不读取状态；`Apply` 复用同一
  `validateBatch`，保证结构语义一致。
- `stats.go` — `Stats`：在读锁内返回线性一致的 Generation/NextRevision/Accounts。
- `clone.go` — `Clone`：深拷贝账户映射并保留逻辑时钟（generation、nextRevision），
  副本与原对象所有权完全隔离。
- `preview.go` — `Preview`：在写锁保护的一次线性化快照上构建候选状态，复用
  `applyBatch` 的完整事务语义，返回候选 `Result`/`Snapshot`/`Stats`；失败时返回
  与 `Apply` 相同的错误且全部返回值为零值，原对象与逻辑时钟不变。

## 索引与候选事务

账户存储为 `map[string]Account` 哈希索引，按名 O(1) 定位；无有序索引，
`Top`/`Snapshot` 按需排序。`Apply` 先在候选映射（原状态的浅拷贝，Account 为值
类型）上顺序执行整批操作，仅在全部成功且批次末容量检查通过后才一次性替换
内部状态指针——这就是“候选事务”：中间失败自然回滚，无需逆向补偿。`Preview`
则把候选事务跑在私有克隆状态上，等价于在同一线性化点上真实提交的结果。

## 所有权

所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot.Accounts`）均为新建
切片；`Account` 为纯值类型，map 拷贝即深拷贝。`Clone`/`Preview` 的候选状态与
原对象不共享任何可写内存。

## 复杂度

- `Apply`/`Preview`：O(n + a)，n 为批内操作数，a 为当前账户数（候选拷贝）。
- `ValidateBatch`：O(n)，纯结构校验。
- `Top(k)`：O(a log a)；`Snapshot`：O(a log a)（按名排序）；`Stats`：O(1)。
- `Clone`：O(a)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
