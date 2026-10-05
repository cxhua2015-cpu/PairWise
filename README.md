# resourcelease149

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

三个生产文件协同工作：

- `heartbeat.go` — 状态引擎 `Table`：事务化数据与快照。
- `policy.go` — 准入策略 `Policy`：独立同步、可原子替换的 actor 白名单与单批操作数上限。
- `coordinator.go` — 协调层 `Coordinator`：先授权再调用引擎，并为成功、拒绝、引擎失败分配连续审计序号。

## 索引

`Table` 以 `map[string]Entry` 为主索引，键即租约名，Put/Touch/Delete 均为 O(1) 均摊查找。条目按 `ExpiresAt` 惰性淘汰：Apply 在候选状态上先删除 `ExpiresAt <= Now` 的条目，Expire 使用同一闭区间边界，不维护额外的堆或定时器。

## 候选事务

Apply 先在完整批次上做结构校验（未知 kind、非法键、负时间 → `ErrInvalidInput`），再检查时间单调性（`ErrTime`）。随后在候选 map 副本上顺序执行 Put/Touch/Delete，Put/Touch 递增分配 revision。最终容量超限（`ErrCapacity`）或任何错误都会整体回滚——淘汰、时间与 revision 均不生效；只有全部成功才交换候选、推进时间并将 generation 加一（空批次不加）。

## 所有权

所有公开方法在内部互斥锁下执行，可并发调用。`Snapshot` 与 `Expire` 返回的切片均为新建副本，`Coordinator.Decisions` 返回审计日志的拷贝，调用方无法别名或篡改内部状态。`Policy.ReplaceActors` 先构建新白名单再在锁内整体替换，进行中的 `Authorize` 只会看到旧表或新表之一。策略拒绝发生在读取或修改核心状态之前。

## 复杂度

- Apply：O(n + b)，n 为现存条目数（候选复制与淘汰扫描），b 为批内操作数。
- Expire：O(n + k log k)，k 为到期条目数（结果按键排序）。
- Snapshot：O(n log n)（排序后拷贝）。
- Authorize / ReplaceActors：O(1) / O(a)，a 为白名单大小。
- Coordinator.Apply：在引擎成本上仅增加 O(1) 审计追加。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
