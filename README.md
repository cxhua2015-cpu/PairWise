# Watermark Join

这是一个用于 Pair-wise 评测的 Go 项目骨架。行为合同见 `SPEC.md`。

实现完成后请在这里记录索引结构、批量事务、容量计数、Payload 所有权和复杂度。

## 实现说明

### 索引结构

每个 Side 各维护一个 `sideState`：

- `byID map[string]*storedEvent`：按 ID 索引，用于幂等重复与冲突检测（O(1)）。
- `byKey map[string]map[string]*storedEvent`：按 Key → ID 二级索引，新事件只扫描对侧相同
  Key 的事件做窗口匹配，不触碰无关 Key。

`storedEvent` 一旦插入即不可变（只增不删改），因此事务克隆可以安全地共享事件指针。

### 批量事务与原子性

`Apply` 就是单元素 `ApplyBatch`。`ApplyBatch` 在单个互斥锁下执行：

1. 克隆当前 `state`（水位线、计数，以及两个 `sideState` 的 map 结构；事件体共享指针）。
2. 按输入顺序在克隆上逐个应用更新，批次内后续更新可见先前结果。
3. 任一步失败（验证、迟到、冲突、水位线回退）或最终容量超限，直接丢弃克隆，
   原状态零副作用，已准备的 Matches/Expired 不会泄漏，重试不会丢失输出。
4. 全部成功且最终 `Count <= MaxEvents && Bytes <= MaxBytes` 时，原子换入新状态。
   容量只在提交点检查，因此批次中途允许暂时超限，再由水位线回收回到预算内。

### 容量计数

`Count` 为活跃事件数；`Bytes` 为所有活跃事件 `len(Key)+len(ID)+len(Payload)` 之和，
随插入/过期增量维护，与 `Snapshot.Events` 逐条重算的结果一致。等于上限合法。

### Payload 所有权

插入时用 `bytes.Clone` 复制输入 Payload；构造 `Match` 时再次克隆，返回值与内部状态完全
隔离。调用方修改输入 slice 或既往返回值，不影响未来结果与快照。`Snapshot` 每次新建
slice，不含 Payload，只记录按定义计算的 `Bytes`。

### 并发

所有公开方法由同一把 `sync.Mutex` 串行化，可线性化、无数据竞争；不读墙钟、不访问网络、
不启动后台 goroutine。

### 时间与空间复杂度

设 N 为活跃事件数，K 为对侧同 Key 事件数，M 为单次产出的匹配/过期条数，B 为批次长度：

- `Apply`（事件）：克隆 O(N) + 匹配 O(K) + 排序 O(M log M)。
- `Apply`（水位线）：克隆 O(N) + 过期扫描 O(N) + 排序 O(M log M)。
- `ApplyBatch`：一次克隆 O(N) + 逐条更新合计 O(B·(K + N))（水位线条目扫描全量）。
- `Snapshot`：O(N log N) 排序，O(N) 额外空间。
- 空间：O(N)（事件体 + 两级索引）。

注：事务克隆为 O(N) 的 map 复制（共享不可变事件指针，不复制 Payload）。这是简单性与
正确性（零副作用回滚）之间的取舍；如需更优增量性能，可改为写前日志式回滚。
