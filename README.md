# expirytable254

并发安全的内存型“到期状态表”，Go 1.22+，仅依赖标准库。表使用显式非负单调时间：
`Apply` 先做完整结构校验，再检查时间单调性；在候选状态上先淘汰 `ExpiresAt <= Now`
的条目，再顺序执行 Put/Touch/Delete；Put/Touch 分配递增 revision。任何错误（包括
最终容量超限）都会将淘汰、时间与 revision 一并回滚。`Expire` 使用相同的闭区间边界
`ExpiresAt <= now`。

## 多文件架构

- `heartbeat.go` — 核心事务引擎：`New`/`Apply`/`Expire`/`Snapshot`，持有互斥锁与主索引。
- `validation.go` — 无副作用的批次预检：`ValidateBatch` 与 `Apply` 共享同一套结构语义
  （键字符集与字节上限、`Now >= 0`、Put/Touch 要求 `ExpiresAt > Now`、未知 kind 拒绝），
  不读取也不修改任何状态。
- `stats.go` — `Stats` 在同一把锁内生成线性一致的状态摘要。
- `clone.go` — `Clone` 深拷贝全部逻辑时钟（now/generation/revision）与索引，
  与原表完全隔离所有权。

## 索引

主索引为 `map[string]Entry`，键到条目 O(1) 定位；条目内联 `ExpiresAt` 与 `Revision`，
不维护额外的堆或时间轮，淘汰采用全量扫描换取实现的确定性与回滚的简单性。

## 候选事务

`Apply` 在锁内克隆索引得到候选状态，在候选上执行淘汰与全部操作，只有最终容量
检查通过才整体提交（替换 map 指针并推进时钟）；任一失败直接丢弃候选，已发生的
淘汰、时间与 revision 分配随之回滚。空批次只推进时间，不增加 generation；非空
成功批次 generation 恰好加一。

## 所有权

所有公开方法可并发调用（单互斥锁保护全部内部状态）。`Snapshot`/`Expire` 返回的
切片均为新建拷贝，与内部状态隔离；`Clone` 返回的表不共享任何可变的底层存储，
对任一表的修改对另一表不可见。

## 复杂度

- `Apply`：O(n + m)，n 为当前条目数（候选克隆与淘汰扫描），m 为批内操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（结果按键排序）。
- `Snapshot`/`Clone`：O(n)（快照另含 O(n log n) 排序）。
- `Stats`/`ValidateBatch`：O(1) / O(m)，均不分配或仅常量级分配。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
