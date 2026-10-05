# resourceledger177

并发安全的内存型“资源计量账本”（Go 1.22+，仅标准库）。原子批次按输入顺序执行
`Add`/`Set`/`Delete`，失败整体回滚。详见 `SPEC.md`。

## 设计要点

- **索引**：账户存储为 `map[string]Account`，按名称 O(1) 定位；`Top` 与
  `Snapshot` 在读取时物化切片并排序，不维护持久有序索引，写路径保持 O(1)。
- **候选事务**：`Apply` 先完整结构校验（kind、名称字符集与字节上限），再在
  账户映射的克隆（候选事务）上按序执行操作；任一步失败直接丢弃克隆，原状态
  零改动，天然实现整体回滚。成功时一次性换入候选映射。
- **所有权**：所有公开方法返回的切片（`Result.Changed`、`Top`、`Snapshot`）
  均为新建并拷贝的切片，与内部状态完全隔离；调用方修改不影响账本。
- **并发**：单把 `sync.RWMutex`；`Apply` 持写锁，`Top`/`Snapshot` 持读锁，
  全部公开方法可并发调用。

## 语义

- `Add`/`Set` 分配连续 revision（从 1 开始）；`Delete` 不消耗 revision。
- 非空成功批次 `generation` 只增一次；空批次不推进 generation 与 revision。
- 算术前检测 int64 溢出，并执行 `MaxAbsValue` 绝对值上限（违反返回 `ErrValue`）。
- 账户容量 `MaxAccounts` 仅在批次末检查（违反返回 `ErrCapacity`）。
- `Top` 按数值降序、名称升序；`Snapshot` 按名称升序。

## 复杂度

- `Apply`：O(n·a) 克隆 + O(n) 执行，n 为批内操作数，a 为账户总数。
- `Top`：O(a log a)；`Snapshot`：O(a log a)。
- 空间：O(a)，`Apply` 期间临时 O(a)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
