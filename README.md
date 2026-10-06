# expirytable239

并发安全的内存型“到期状态表 239”，仅依赖 Go 标准库（Go 1.22+）。表由显式、非负、单调不减的逻辑时钟驱动；`Apply` 先结构校验再检查时间，在候选状态上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 `Put`/`Touch`/`Delete`，任何错误（含最终容量超限）都会连同淘汰、时间与 revision 一起回滚。`Expire` 使用相同的闭区间边界。

## 多文件架构

- `heartbeat.go` — 核心事务引擎：`Table` 状态、`New`/`Apply`/`Expire`/`Snapshot`。
- `validation.go` — 无副作用的批次预检：`validateBatch` 只依赖不可变配置，不读不写状态；`Apply` 与 `ValidateBatch` 共享同一套结构语义（键字符集/字节上限、`ExpiresAt > Now`、合法 kind、非负 Now）。
- `stats.go` — 线性一致统计：`Stats` 在同一把互斥锁内取数，与并发事务状态保持一致。
- `clone.go` — 深拷贝：`Clone` 复制全部逻辑时钟（now/generation/nextRevision）与条目，与源表完全隔离所有权。

## 索引

条目存放在 `map[string]Entry` 哈希索引中，按键精确查找；`Snapshot`/`Expire` 返回的切片按键排序以保证确定性，且与内部状态完全隔离。

## 候选事务

`Apply` 在持锁后先复制一份候选 map，在候选上执行淘汰与全部操作，最后做容量检查；只有全部成功才整体替换内部状态并推进时钟与 revision，否则原状态、淘汰结果、时间和 revision 计数全部保持不变。非空成功批次 generation 只增加一次，空批次不增加（但仍推进时钟并执行淘汰）。

## 所有权

所有公开方法均可并发调用（内部由单一 `sync.Mutex` 串行化）。`Snapshot`、`Expire` 返回的切片为新分配内存；`Clone` 的 map 为独立副本，修改克隆体不会影响原表，反之亦然。

## 复杂度

- `Put`/`Touch`/`Delete` 单操作：O(1) 均摊。
- `Apply`：O(n + m)，n 为当前条目数（候选复制与淘汰），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序输出）。
- `Snapshot`/`Clone`：O(n log n) / O(n)。
- `Stats`/`ValidateBatch`：O(1) / O(m)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
