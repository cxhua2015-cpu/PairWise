# metacatalog276

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。原子批次按输入顺序执行
Put/Delete：Put 分配连续 revision，Delete 不分配；完整结构校验先于状态读取；
记录数与 Value 总字节容量只在批次末检查；任何失败回滚全部状态、generation
与 revision。`Get`/`Snapshot` 深拷贝 Value，`Snapshot` 按名称排序。

## 多文件架构

- `servicecatalog.go` — 核心事务引擎：`Store`、`New`、`Apply`、`Get`、`Snapshot`。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 与 `Apply` 共享同一套
  结构语义（kind 合法、名称字符集/长度、Put 值长上限、Delete 必须无 Value），
  不读取也不修改任何状态。
- `stats.go` — 线性一致统计：`Stats` 在读锁下返回 generation、nextRevision、
  记录数与 Value 总字节的一致快照。
- `clone.go` — 所有权隔离的深拷贝：`Clone` 保留逻辑时钟（generation、
  nextRevision），但与原 Store 不共享任何内存。

## 设计说明

- **索引**：记录存放在 `map[string]Record` 哈希索引中，按名称 O(1) 定位；
  同时冗余维护 `totalBytes` 计数器，使容量检查与 `Stats` 均为 O(1)。
- **候选事务**：`Apply` 在写锁内把当前 map 浅拷贝为候选状态，按输入顺序
  在候选上重放所有 Op；任一步失败（`ErrNotFound`/`ErrCapacity`）直接丢弃
  候选即完成回滚，成功后整体换入并仅递增一次 generation。空批次不改变
  generation。
- **所有权**：Put 时拷贝输入 Value；`Get`/`Snapshot`/`Result.Changed`/`Clone`
  均返回深拷贝，调用方对返回切片的任何修改都不会影响内部状态，反之亦然。
- **并发**：单把 `sync.RWMutex`；写路径（`Apply`）独占，读路径
  （`Get`/`Snapshot`/`Stats`/`Clone`）共享读锁，`ValidateBatch` 无锁纯函数。
- **复杂度**：`Apply` 为 O(n + m log m)（n 为批内 Op 数，m 为变更名数，排序
  产出 `Changed`）；`Get`/`Stats` 为 O(1)；`Snapshot`/`Clone` 为 O(r log r) /
  O(r)（r 为记录数，含值拷贝的总字节开销）。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
