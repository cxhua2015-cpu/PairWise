# metacatalog371

并发安全的内存型元数据目录，实现见 `metacatalog371/servicecatalog.go`，语义以 `SPEC.md` 与契约测试为准。仅依赖标准库，需 Go 1.22+。

## 设计说明

**索引**
- 主索引为 `map[string]entry`，`entry` 保存深拷贝后的 Value 与该记录的 revision。
- 另维护 `totalBytes`（Value 总字节）、`generation`、`revision` 三个计数器，避免每次遍历时聚合。
- 无单独有序索引：`Snapshot`/`Changed` 在返回前按名称排序，写路径保持 O(1)。

**候选事务（candidate transaction）**
- `Apply` 先做整批结构校验（kind、名称字符集与长度、Value 长度、Delete 不带 Value），不读取任何状态。
- 校验通过后在当前记录的私有副本上按输入顺序执行 Put/Delete：Put 分配连续 revision，Delete 不分配；Delete 缺失键返回 `ErrNotFound`。
- 记录数与 Value 总字节容量只在批次末检查，超限返回 `ErrCapacity`。
- 任一步失败直接丢弃副本，状态、generation、revision 全部不变（天然回滚）；成功时整体提交，非空批次 generation 只加一。

**所有权**
- Put 时拷贝输入 Value；`Get`/`Snapshot`/`Result.Changed` 返回的 Value 均为深拷贝，调用方对返回切片的修改不影响内部状态，反之亦然。

**并发**
- 单把 `sync.RWMutex`：`Apply` 持写锁，`Get`/`Snapshot` 持读锁，所有公开方法可并发调用。

**复杂度**（n = 记录数，k = 批次内 op 数）
- `Apply`：O(n + k) 复制候选副本，O(k log k) 排序 `Changed`。
- `Get`：O(1)（外加返回值拷贝）。
- `Snapshot`：O(n log n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
