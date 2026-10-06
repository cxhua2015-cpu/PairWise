# metacatalog271

并发安全的内存型元数据目录，仅依赖标准库（Go 1.22+）。原子批次按输入顺序执行 Put/Delete，Put 分配连续 revision，Delete 不分配；失败回滚全部状态、generation 与 revision。

## 多文件架构

- `servicecatalog.go` — 核心事务引擎：`New`/`Apply`/`Get`/`Snapshot`，索引与逻辑时钟。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 与 `Apply` 共享同一套结构语义（`validateOp`/`validateName`），只读 Options，不触碰状态。
- `stats.go` — 线性一致的状态统计：`Stats` 在读锁下返回一致的 generation、nextRevision、记录数与 Value 总字节。
- `clone.go` — 保留逻辑时钟的深拷贝：`Clone` 复制全部记录与计数器，所有权完全独立。

## 设计说明

**索引**：单把 `sync.RWMutex` 保护 `map[string]Record` 主索引，另维护 `totalValue` 累计值避免每次 O(n) 求和。`Record` 为不可变值，Put 时整体替换，从不原地修改 `Value`。

**候选事务**：`Apply` 先做完整结构校验（不读状态），再在写锁内把索引浅拷贝为候选 map，按输入顺序应用全部 op（Put 从本地 `nextRev` 连续取号，Delete 不取号），批次末才检查记录数与总字节容量。任何失败直接丢弃候选，已提交状态、generation、revision 天然不变；成功则整体换入候选并仅递增一次 generation（空批次不递增）。

**所有权**：Put 的 `Value` 入库前拷贝；`Get`/`Snapshot`/`Result.Changed` 返回的 `Value` 均为深拷贝，调用方无法读写内部状态。`Clone` 逐条深拷贝记录，副本与原件互不影响，但保留相同的 generation 与 nextRevision。

**复杂度**：结构校验 O(批次字节数)；`Apply` 为 O(n + k)（n 为现有记录数的浅拷贝、 k 为 op 数），外加 `Changed` 排序 O(k log k)；`Get` O(1)；`Snapshot`/`Clone` O(n log n) / O(n)；`Stats` O(1)。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
