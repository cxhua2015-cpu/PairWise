# metacatalog416

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

实现刻意拆分为四个相互协作的文件：

- `servicecatalog.go` — 核心事务引擎：`Store`、`Apply`、`Get`、`Snapshot`。
- `validation.go` — 无副作用的批次结构预检 `ValidateBatch`，与 `Apply` 共享同一套结构语义（kind、名称字符集与长度、Value 约束）。
- `stats.go` — 线性一致的状态统计 `Stats`（读锁下的一次性快照）。
- `clone.go` — 保留逻辑时钟（generation / nextRevision）且所有权完全隔离的深拷贝 `Clone`，以及共用的 `cloneBytes`。

## 索引

记录存储在以名称为键的 `map[string]Record` 中，Put/Delete/Get 均为 O(1) 均摊查找。`Snapshot` 与 `Apply` 的 `Changed` 在返回前按名称排序，保证确定性输出。

## 候选事务（staging）

`Apply` 先调用 `ValidateBatch` 做完整结构校验（不读取任何状态），再在写锁内把当前记录复制到候选 map 上按输入顺序重放整批操作：Put 分配连续 revision，Delete 不分配。记录数与 Value 总字节容量只在批次末检查；任何失败（`ErrNotFound` / `ErrCapacity`）直接丢弃候选状态，generation 与 revision 均不回退泄漏。全部成功才一次性提交并令 generation 恰好加一；空批次不改变 generation。

## 所有权

所有进出边界的 `Value` 都经过深拷贝：Put 存入前拷贝，`Get`/`Snapshot`/`Result.Changed` 返回前拷贝，`Clone` 整体拷贝。调用方对返回切片的修改、或提交后对输入切片的修改，都不会影响目录内部状态；克隆体与原Store完全独立。

## 并发与复杂度

- 单把 `sync.RWMutex` 保护全部状态：`Apply` 持写锁，`Get`/`Snapshot`/`Stats`/`Clone` 持读锁，因此 `Stats` 与 `Clone` 相对并发事务是线性一致的。
- `ValidateBatch` 是纯函数，无需锁。
- 设批次含 k 个 op、目录含 n 条记录：`Apply` 为 O(n + k)（候选复制 + 重放 + 排序 O(k log k)），`Get` O(1)，`Snapshot`/`Clone` O(n log n) / O(n)，`Stats` O(1)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
