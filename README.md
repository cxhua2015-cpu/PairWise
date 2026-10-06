# readyqueue205

并发安全的内存型“就绪优先队列”，仅依赖标准库（Go 1.22+）。队列使用显式非负单调时间：`Apply` 原子顺序执行 `Enqueue`/`Cancel`，`Pop` 按规范顺序取回已就绪任务并原子删除。

## 设计

### 索引

- 主索引为 `map[string]Item`，按 ID 提供 O(1) 的存在性判定、入队与取消。
- 规范顺序（Priority 降序、ReadyAt 升序、ID 升序）不维护持久堆，而是在 `Pop`/`Snapshot` 时对候选集即时排序。队列规模受 `MaxItems` 上限约束，惰性排序换取写入路径 O(1) 与实现的简单可靠。

### 候选事务

- `Apply` 先做整批结构校验（时间非负、kind 合法、ID 字符集与字节上限），不读取任何状态。
- 通过校验后在当前 map 的克隆上顺序执行各 op（Enqueue 分配递增 revision，Cancel 删除），容量只在末尾检查一次。
- 任一步失败（`ErrExists`/`ErrNotFound`/`ErrCapacity`）直接丢弃克隆，时间、状态、revision、generation 全部天然回滚；成功才整体换入并推进 `now` 与 `generation`（非空批次 +1，空批次不变）。

### 所有权与并发

- 所有公开方法由同一把 `sync.Mutex` 保护，可任意并发调用。
- 返回值（`[]Item`、`Snapshot.Items`）均为每次调用新建的切片，`Item` 为值类型，调用方修改不会触及内部状态；内部 map 在成功 `Apply` 后整体替换，旧克隆归调用方帧所有，无共享可变内存。

### 复杂度

- `Apply`：O(k·n) 克隆 + O(k) 执行，k 为批次大小，n 为当前元素数。
- `Pop`：O(n + r log r)，r 为就绪候选数；删除 O(r)。
- `Snapshot`：O(n log n) 排序输出。
- 空间：O(n)。

## 验证

```sh
go test ./...
go test -race ./...
go run ./cmd/demo
```
