# expirytable239

并发安全的内存型“到期状态表”，仅依赖标准库（Go 1.22+）。表使用显式非负单调时间：
`Apply` 先结构校验再检查时间，在候选状态上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行
Put/Touch/Delete；任何错误（含最终容量失败）都会连同淘汰、时间与 revision 一起回滚。
`Expire` 使用相同的闭区间边界。

## 多文件架构

- `heartbeat.go` — 核心事务引擎：`Table` 状态、`New`、`Apply`、`Expire`、`Snapshot`。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 与 `Apply` 共享同一套结构语义
  （键字符集/长度、kind 合法性、非负时间、Put/Touch 的 `ExpiresAt > Now`），不读取任何状态。
- `stats.go` — `Stats` 在同一把互斥锁下汇总，保证线性一致，绝不观察到半截事务。
- `clone.go` — `Clone` 深拷贝全部状态与逻辑时钟（now/generation/nextRevision），
  克隆体与原表零共享，互不影响。

## 索引与候选事务

- 主索引为 `map[string]Entry`，按键 O(1) 定位；无堆结构，淘汰采用全量扫描。
- `Apply` 在候选 map 上执行“淘汰 + 顺序 ops + 最终容量检查”，全部成功才整体替换
  内部 map 并推进时钟与 revision，因此回滚是免费的（直接丢弃候选）。
- 非空成功批次 generation 恰好 +1；空批次不改变 generation。

## 所有权与并发

- 所有公开方法通过单把 `sync.Mutex` 串行化，支持并发调用。
- `Snapshot`/`Expire` 返回的切片均为新建拷贝，与内部状态隔离；`Clone` 不共享任何内存。

## 复杂度

- `Apply`：O(n + m)，n 为现有条目数（候选复制与淘汰扫描），m 为批次内 op 数。
- `Expire`：O(n + k log k)，k 为到期条目数（结果按键排序）。
- `Snapshot`/`Clone`：O(n log n) / O(n)；`Stats`：O(1)。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
