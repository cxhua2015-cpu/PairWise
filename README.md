# resourcelease134

并发安全的内存型资源租约表（Go 1.22+，仅标准库）。实现见 `SPEC.md`，分为三个协同的生产文件。

## 架构

- `heartbeat.go`（状态引擎）：持有事务性数据与快照。`Apply` 先完整结构校验（kind、键字符集 `a-z0-9-_`、字节上限、非负时间），再检查单调时间；然后在**候选状态**（entries 与索引的副本）上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 Put/Touch/Delete，Put/Touch 分配递增 revision。容量超限或任何错误整体回滚（淘汰、时间、revision、generation 均不落盘）。非空成功批次 generation 恰好 +1，空批次只推进时间与淘汰、不改 generation。`Expire` 使用相同闭区间边界并推进时间。
- `policy.go`（策略层）：独立同步（`sync.RWMutex`）的准入配置。actor 白名单通过 `ReplaceActors` **原子整体替换**（先构建新 map 再一次性换入）；`Authorize` 校验 actor 是否在白名单且单批操作数不超过上限，失败返回 `ErrDenied`，绝不读写核心状态。
- `coordinator.go`（协调层）：串行化准入——先 `Authorize`，再委托状态引擎；为每次成功、拒绝或引擎失败追加**连续递增**的审计序号（`Decision.Sequence` 从 1 起无间隙）。`Decisions()` 返回副本切片，与内部存储所有权隔离。

## 索引与复杂度

- 状态引擎维护 `[]Entry`（保持插入序，供快照）加 `map[string]int` 键→位置索引。
- Put/Touch/Delete 均摊 O(1) 定位；Delete 用切片删除，最坏 O(n) 搬移；每批先 O(n) 过期扫描；整体单批复杂度 O(n + 批内操作数)。
- Snapshot/Decisions 为 O(n) 深拷贝，返回切片不别名内部状态，调用方持有独立所有权。

## 并发与所有权

三层各自持有互斥锁，所有公开方法可并发调用；策略替换、协调调用、核心方法与审计读取互不安全竞争（`go test -race` 验证）。错误路径不泄露部分变更；返回值均为拷贝。

## 验证

```
go test ./...
go test -race ./...
go run ./cmd/demo
```
