# expirytable339

并发安全的内存型“到期状态表 339”（Go 1.22+，仅标准库）。语义详见 `SPEC.md`。

## 设计说明

### 索引
- 主索引为 `map[string]Entry`，键即条目 Key，Put/Touch/Delete 与过期扫描均为哈希访问。
- 未维护额外的堆/时间轮：`Expire` 与 Apply 的候选淘汰直接全表扫描，条目数为 `MaxEntries` 有界，实现更简单且无索引一致性风险。
- `Snapshot`/`Expire` 返回前按 Key 排序，保证规范顺序（canonical order）。

### 候选事务（candidate transaction）
- `Apply` 分三段：先对整个批次做**完整结构校验**（kind 合法、键字符集与字节上限、时间非负），再检查时间单调性（`Now < now` → `ErrTime`）。
- 然后在候选副本（`map` 浅拷贝）上先删除 `ExpiresAt <= Now` 的条目（闭区间），再顺序重放 Put/Touch/Delete；Put/Touch 各自分配一个递增 revision。
- 最终条目数超过 `MaxEntries` 或任何一步出错（`ErrNotFound` 等）时直接丢弃候选：淘汰、时间、revision、generation 全部回滚，已持有状态不受影响。
- 仅在全部成功后提交：替换索引、推进 `now` 与 `nextRevision`；非空批次 `generation` 恰好 +1，空批次不变（但仍推进时间并执行淘汰）。
- `Expire(now)` 使用同一闭区间边界 `ExpiresAt <= now`，单调推进 `now` 并返回被移除条目。

### 所有权
- 所有公开方法通过单一 `sync.Mutex` 串行化，支持任意并发调用。
- 返回值（`Result`、`Snapshot`、`[]Entry`）均为按值拷贝/新建切片，与内部状态完全隔离；调用方修改返回的切片或条目不影响表。

### 复杂度
- `Apply`：O(E + B)，E 为当前条目数（候选拷贝 + 淘汰扫描），B 为批内 op 数。
- `Expire`：O(E + K log K)，K 为到期条目数（排序）。
- `Snapshot`：O(E log E)（拷贝后按 Key 排序）。
- 空间：O(E)。

## 验证
```
go test ./...
go test -race ./...
go run ./cmd/demo
```
