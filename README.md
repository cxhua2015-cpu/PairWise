# resourcelease134

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

三个生产文件协同工作：

- `heartbeat.go`（状态引擎）：持有租约数据、单调时间与 revision，提供事务性 `Apply`/`Expire`/`Snapshot`。
- `policy.go`（策略层）：独立同步的 actor 白名单（`ReplaceActors` 原子整体替换）与单批操作数上限，`Authorize` 只做准入判断，不触碰核心状态。
- `coordinator.go`（协调层）：串行化准入，先 `Authorize` 再委托状态引擎，并为成功、拒绝与引擎失败分配连续审计序号。

## 索引

状态引擎使用 `map[string]Entry` 作为主索引，键即租约键。`Snapshot`/`Expire` 返回按键排序的切片，保证输出确定性。

## 候选事务

`Apply` 先对整批做结构校验（kind、键字符集与字节上限），再在锁内检查时间单调性；随后在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete 并分配 revision，最后做容量检查。任一步失败直接丢弃候选，淘汰、时间与 revision 全部回滚；成功才一次性提交。非空成功批次 generation 只加一，空批次不改变状态。

## 所有权

`Snapshot`、`Expire`、`Coordinator.Decisions` 返回的切片均为新分配的副本，调用方修改不会影响内部状态；`Policy.ReplaceActors` 将白名单复制进新 map 后原子替换，不保留调用方切片引用。

## 复杂度

- `Apply`：O(E + B)，E 为当前条目数（候选复制与淘汰扫描），B 为批内操作数。
- `Expire`：O(E + K log K)，K 为过期条目数（排序输出）。
- `Snapshot`：O(E log E)（排序输出）。
- `Authorize`/`ReplaceActors`：O(1) / O(A)，A 为 actor 数。
- `Coordinator.Apply` 在单把互斥锁下串行，审计追加 O(1) 均摊。

## 并发

三层各自使用互斥锁/读写锁保护，所有公开方法可并发调用；`go test -race ./...` 通过。
