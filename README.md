# resourcelease139

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构

包内三个生产文件协同工作：

- `heartbeat.go`（状态引擎）：持有条目、单调时间、generation 与 revision，
  提供事务性 `Apply`、闭区间 `Expire` 与一致性 `Snapshot`。
- `policy.go`（准入策略）：维护可原子替换的 actor 白名单与单批操作数上限。
  配置整体存放在 `atomic.Pointer` 中，`ReplaceActors` 一次性换入新配置，
  `Authorize` 永远不会观察到替换到一半的白名单。
- `coordinator.go`（协调层）：串行化准入——先 `Authorize`，再调用状态引擎，
  并为成功、拒绝与引擎失败分配连续递增的审计序号（`Decision.Sequence`）。
  策略拒绝不会读取或修改核心状态。

## 索引

条目存储在 `map[string]Entry` 中，按键 O(1) 定位；`Snapshot`/`Expire`
返回的切片按键排序以保证确定性。未维护额外的过期堆——容量上限较小且
`Apply` 每次都会全量淘汰，排序成本可接受。

## 候选事务

`Apply` 先完整结构校验（kind、键字符集与字节上限、`ExpiresAt` 非负），
再检查时间单调性（`Now >= now`，负 `Now` 视为时间错误）。随后在候选副本上
先删除 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete，Put/Touch
分配递增 revision。最终容量超限或任何错误都会整体回滚——淘汰、时间与
revision 均不落盘。非空成功批次 generation 恰好加一，空批次不变。

## 所有权

所有公开方法返回值（`Snapshot.Entries`、`Expire`、`Decisions`）均为新建切片，
与内部状态完全隔离；调用方修改返回切片不影响表或审计日志。

## 并发与复杂度

三层各自独立同步：状态引擎与协调层使用互斥锁，策略层使用原子指针，
全部公开方法可并发调用（`-race` 通过）。

- `Apply`：O(n + m)，n 为现存条目数（候选复制 + 淘汰），m 为批内操作数。
- `Expire` / `Snapshot`：O(n log n)（排序）。
- `Authorize` / `ReplaceActors`：O(1) / O(a)，a 为 actor 数。
- `Coordinator.Apply`：在引擎成本上附加 O(1) 审计追加；`Decisions` 为 O(d) 拷贝。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
