# resourcecatalog146

并发安全的内存型资源目录（Go 1.22+，仅标准库）。架构分三层：

- **状态引擎**（`servicecatalog.go`）：`Store` 持有已提交的事务数据与快照。
- **策略层**（`policy.go`）：`Policy` 独立同步，维护可原子替换的 actor 白名单与单批操作数上限。
- **协调层**（`coordinator.go`）：`Coordinator` 串行化准入，先授权再委托状态引擎，并为每次成功、拒绝或引擎失败记录单调递增的审计序号。

## 索引

`Store` 使用 `map[string]Record` 作为主索引，按名称 O(1) 定位记录；`totalValue` 计数器随批次增量维护，避免每次容量检查都全表扫描。`Snapshot` 与 `Result.Changed` 在读取时按名称排序（O(n log n)）。

## 候选事务

`Apply` 先在完整结构校验（kind、名称字符集与长度、Value 长度）通过后，把全部变更暂存到记录的克隆（候选事务）上：Put 分配连续 revision，Delete 校验存在性但不分配 revision。记录数与 Value 总字节容量只在批次末对候选状态检查；任何失败直接丢弃候选，已提交的状态、generation 与 revision 完全不变。非空成功批次 generation 恰好加一，空批次不变。

## 所有权

所有跨越 API 边界的 `Value` 字节切片都做深拷贝：Put 入库时拷贝，`Get`/`Snapshot`/`Result.Changed` 返回时拷贝，调用方对返回切片的修改不会影响内部状态。`Coordinator.Decisions` 返回内部审计日志的独立副本，不共享底层数组。`Policy.ReplaceActors` 将白名单整体构建后再原子换入，读取侧无部分可见状态。

## 复杂度

- `Apply`：O(k·n)（k 为批内操作数，n 为现有记录数，用于克隆候选；另加排序 O(k log k)）。
- `Get`：O(1) 均摊；`Snapshot`：O(n log n)。
- `Authorize`：O(1)；`Decisions`：O(d)（d 为审计条数）。

## 并发

三层各自持有独立互斥锁，所有公开方法可并发调用；`Store` 的单个互斥锁保证批次原子性，`Coordinator` 的锁保证审计序号连续无空洞。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
