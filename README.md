# resourcelease144

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。规范见 `SPEC.md`。

## 架构（三层联动）

- `heartbeat.go` — 状态引擎 `Table`：事务化数据与快照。
- `policy.go` — 准入策略 `Policy`：独立同步、可原子替换的 actor 白名单与单批操作数上限。
- `coordinator.go` — 协调层 `Coordinator`：先授权再调用引擎，并为成功、拒绝、引擎失败分配连续审计序号。

## 索引

`Table` 以 `map[string]Entry` 为主索引，键即租约名。无二级索引：过期淘汰与快照排序均为主索引上的一次扫描。

## 候选事务

`Apply` 在互斥锁内先完整结构校验（kind、键字符集与字节上限、非负时间），再检查单调时间；随后在候选副本上先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete（Put/Touch 分配 revision）。最终容量校验失败或任何错误时整体回滚——淘汰、时间与 revision 均不落盘。非空成功批次 generation 只增一次，空批次不变。`Expire` 使用相同闭区间边界。

## 所有权

所有公开方法返回的切片（`Snapshot.Entries`、`Expire`、`Coordinator.Decisions`）均为新分配的副本，调用方修改不影响内部状态；`Policy.ReplaceActors` 拷贝输入并原子整体替换。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为过期条目数（结果按键排序）。
- `Snapshot`：O(n log n)（排序）；`Authorize`：O(1)；`Decisions`：O(d) 拷贝。

## 并发

三层各自持有互斥锁，全部公开方法可并发调用；审计序号在锁内单调递增且连续。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
