# resourcelease144

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 架构（三层）

- **状态引擎 `heartbeat.go`**：`Table` 持有条目、单调时间、generation 与 revision 计数器，负责事务性写入与快照。
- **策略层 `policy.go`**：`Policy` 独立同步，维护可原子替换的 actor 白名单与单批操作数上限；`Authorize` 只读校验，绝不触碰核心状态。
- **协调层 `coordinator.go`**：`Coordinator` 先授权再委托状态引擎，并为每次成功、拒绝或引擎失败记录连续递增的审计序号。

## 索引

核心索引为 `map[string]Entry`（键 → 租约条目），按键 O(1) 定位。`Snapshot` 与 `Expire` 返回的切片按键名排序，保证确定性输出；不维护额外的按过期时间堆/树，过期通过全表扫描完成（闭区间 `ExpiresAt <= Now`）。

## 候选事务

`Apply` 全程持锁，顺序为：结构校验（kind、键字符集与字节上限）→ 时间单调性检查 → 在**候选副本**上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete（Put/Touch 各分配一个 revision）→ 最终容量检查。任一步失败（`ErrInvalidInput`/`ErrTime`/`ErrNotFound`/`ErrCapacity`）直接丢弃候选，淘汰、时间与 revision 一并回滚，原状态零副作用。非空成功批次 generation 恰好 +1；空批次为无操作，不改变任何状态。

## 所有权与并发

- 三个公开类型各自持有 `sync.Mutex`/`sync.RWMutex`，所有公开方法可并发调用。
- `Snapshot`、`Expire`、`Decisions` 均返回新分配的切片，调用方修改返回值不影响内部状态；`ReplaceActors` 整体替换白名单 map，旧读者不受影响。
- 策略拒绝发生在读取核心状态之前；审计序号在协调层锁内单调分配，成功、拒绝、引擎失败共享同一序列。

## 复杂度

- `Apply`：O(E + B)，E 为当前条目数（候选复制 + 过期扫描），B 为批内操作数。
- `Expire`：O(E + K log K)，K 为过期条目数（排序）。
- `Snapshot`：O(E log E)（排序）。
- `Authorize`：O(1)；`Coordinator.Apply` 额外 O(1) 审计追加；`Decisions`：O(D)，D 为审计条数。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
