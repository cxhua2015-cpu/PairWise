# metacatalog421

并发安全的内存型元数据目录（Go 1.22+，仅标准库）。语义见 `SPEC.md`。

## 架构

实现按职责拆分为五个文件：

- `servicecatalog.go` — 核心事务引擎：`New`/`Apply`/`Get`/`Snapshot`，候选事务提交与回滚。
- `validation.go` — 无副作用的批次结构预检（`ValidateBatch`），与 `Apply` 共享同一套结构语义。
- `stats.go` — 线性一致的 `Stats` 汇总（读锁下的原子视图）。
- `clone.go` — 保留逻辑时钟（generation / nextRevision）且所有权完全隔离的深拷贝。
- `preview.go` — 事务预演：在一次线性化快照上克隆候选对象并复用 `applyLocked`，返回候选 `Result`/`Snapshot`/`Stats`，错误及优先级与同状态 `Apply` 完全一致，不改变原对象任何状态。

## 索引

记录存储在 `map[string]Record` 哈希索引中，按名称 O(1) 定位；`Snapshot` 在读取时收集键并排序，保证按名称有序输出。`totalBytes` 作为冗余聚合随事务增量维护，使容量检查与 `Stats` 均为 O(1)。

## 候选事务

`Apply` 先在候选 map（现有记录的浅拷贝 + 新值的深拷贝）上按输入顺序重放 Put/Delete：Put 分配连续 revision，Delete 要求存在且不分配 revision。批次末才检查最终记录数与 Value 总字节上限。任一步失败直接丢弃候选状态——原 map、generation、revision 均未触碰，天然回滚；成功时整体换入并令 generation 恰好加一（空批次不加）。

## 所有权

所有进入的值（Put）在写入前拷贝，所有离开的值（`Get`/`Snapshot`/`Result.Changed`）在返回前拷贝；`Clone` 逐条深拷贝记录。因此调用方对返回切片的任何修改都不会影响存储，反之亦然；存储内部从不原地修改已存值，使 `Preview` 的候选克隆与试算可以安全共享只读切片。

## 并发与复杂度

单个 `sync.RWMutex` 保护全部状态：写路径（`Apply`）与 `Preview` 取写锁，读路径（`Get`/`Snapshot`/`Stats`/`Clone`）取读锁。`Preview` 在同一把写锁内完成克隆与模拟，因此其结果等价于在该线性化点上真实提交。

- `Apply` / `Preview`：O(n + r)，n 为批内操作数，r 为现有记录数（候选 map 拷贝）。
- `Get` / `Stats`：O(1)。
- `Snapshot` / `Clone`：O(r log r) / O(r)。
- 空间：O(r + 批次内新值字节数)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
