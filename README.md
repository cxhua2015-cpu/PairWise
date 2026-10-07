# expirytable419

并发安全的内存型“到期状态表”，仅依赖 Go 标准库（Go 1.22+）。语义详见 `SPEC.md`。

## 架构（多文件联动）

- `heartbeat.go` — 核心事务引擎：`New` / `Apply` / `Expire` / `Snapshot`，以及共享的结构校验原语。
- `validation.go` — `ValidateBatch`：无副作用的批次预检，与 `Apply` 复用同一套结构语义（键字符集与长度、已知 kind、非负时间、Put/Touch 不得 `ExpiresAt <= Now`），不读取任何表状态。
- `stats.go` — `Stats`：在同一把互斥锁内读取的线性一致统计（generation、next revision、逻辑时钟、条目数）。
- `clone.go` — `Clone`：保留逻辑时钟（now / generation / revision）的深拷贝，条目 map 完全重建，与原表无任何共享所有权。

## 索引

条目存储在以键为索引的 `map[string]Entry` 中，Put/Touch/Delete 均为 O(1) 定位。`Snapshot`/`Expire` 返回的切片按键排序以保证确定性输出。

## 候选事务

`Apply` 先调用 `ValidateBatch` 做完整结构校验，再在锁内检查时间单调性；随后在候选状态（条目的独立副本）上先淘汰 `ExpiresAt <= Now`（闭区间）的条目，再顺序执行 Put/Touch/Delete 并分配 revision。最终容量校验失败或任何错误（`ErrNotFound`/`ErrCapacity`）都会直接丢弃候选状态，淘汰、时间与 revision 一并回滚；成功时才整体提交，非空批次 generation 恰好加一。

## 所有权

所有公开方法可并发调用（单个 `sync.Mutex` 保护全部状态）。`Snapshot`/`Expire` 返回的切片均为新建副本，修改返回值不影响内部状态；`Clone` 后的两张表互不影响。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰），m 为批内 op 数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序输出）。
- `Snapshot` / `Clone`：O(n log n) / O(n)。
- `Stats` / `ValidateBatch`：O(1) / O(m·L)，L 为键长。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
