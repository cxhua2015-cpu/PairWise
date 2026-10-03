# Watermark Join

这是一个用于 Pair-wise 评测的 Go 项目骨架。行为合同见 `SPEC.md`。

## 实现说明

### 索引结构

活跃事件以不可变记录 `rec` 存储，按 Side 分为两组索引：

- `byID[side]`: `map[ID]*rec`，用于 O(1) 的幂等重复 / 冲突检测（ID 作用域为 Side）。
- `byKey[side]`: `map[Key][]*rec`，每个 Key 下的切片按 `(Time, ID)` 升序排列，匹配时只扫描相同 Key 的对侧事件，不触碰无关 Key；由于切片本身有序，匹配结果天然按对侧 `Time, ID` 升序，无需再排序。

水位线过期按合同允许扫描对侧活跃集合并按 `(Time, Key, ID)` 排序输出。快照按 `(Side, Time, Key, ID)` 排序。

### 事务原子性

所有公开方法由单个 `sync.Mutex` 串行化，可线性化。`ApplyBatch` 先对当前状态做浅克隆（记录不可变、索引切片写时复制，因此浅克隆即是有效检查点），在克隆上按顺序执行全部更新；任一步失败或最终容量超限时直接丢弃克隆，原状态零副作用，已准备的 Matches/Expired 不会泄漏。`Apply` 等价于单元素 `ApplyBatch`。容量（`MaxEvents`/`MaxBytes`）只在整批结束后检查，因此批次中途可暂时超限，再由水位线回收回到预算内。

### 容量计数

`Count` 为活跃事件数；`Bytes` 为所有活跃事件 `len(Key)+len(ID)+len(Payload)` 之和，在插入/删除时增量维护，等于上限合法。

### Payload 所有权

插入时复制输入 Payload；返回 `Match` 的 Left/Right 时也复制内部 Payload。调用方修改输入或既往返回值均不影响内部状态、未来结果与快照。`Snapshot.Events` 每次新建切片，不含 Payload，只含按定义计算的 `Bytes`。

### 复杂度

设 n 为活跃事件数，k 为某 Key 下对侧事件数，m 为匹配数，x 为单次过期数，B 为批次长度。

- 事件插入：匹配 O(k)，插入有序切片 O(k)（均摊 memmove），空间 O(1) 额外（不计输出 O(m)）。
- 水位线：扫描 O(n) + 排序 O(x log x) + 删除 O(x·k)。
- `ApplyBatch`：各更新之和，外加事务克隆 O(n)。
- `Snapshot`：O(n log n) 排序，空间 O(n)。
- 总空间：O(n)（含 Payload 字节）。

实现不读取墙钟、不访问网络、不启动后台 goroutine，仅依赖标准库。
