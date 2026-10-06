# expirytable269

并发安全的内存型“到期状态表 269”，仅依赖 Go 标准库（Go 1.22+）。语义见 `SPEC.md`。

## Multi-file architecture

实现按职责拆分为四个联动文件，共享同一套结构语义与逻辑时钟：

- `heartbeat.go` — 核心事务引擎：`New` / `Apply` / `Expire` / `Snapshot`。
- `validation.go` — 无副作用批次预检：`ValidateBatch` 只做结构校验（非负时间、合法 kind、键字符集与字节上限、`Put/Touch` 要求 `ExpiresAt > Now`），不读不改状态；`Apply` 复用同一套校验。
- `stats.go` — 线性一致统计：`Stats` 与事务共用同一把互斥锁，绝不观察到半提交的候选状态。
- `clone.go` — 深拷贝：`Clone` 复制逻辑时钟（now/generation/nextRevision）、配置与全部条目，与源表完全隔离所有权。

## 索引

主索引是 `map[string]Entry` 哈希表，按键定位 O(1)。`Snapshot`/`Expire` 返回的切片按键排序，保证确定性输出，且均为新建副本，不别名内部状态。

## 候选事务

`Apply` 先结构校验、再检查单调时钟，然后在候选副本上先淘汰 `ExpiresAt <= Now` 的条目，再顺序执行 `Put/Touch/Delete`（`Put/Touch` 分配 revision）。最终容量超限或任何错误会整体回滚淘汰、时间与 revision；只有全部成功才一次性提交并令 generation 恰好加一（空批次不变）。`Expire` 使用相同的闭区间边界并推进时钟。

## 所有权

所有公开方法返回的切片与 `Clone` 结果都是深拷贝，调用方与表之间、源表与克隆体之间不共享任何可写内存。

## 复杂度

- `Apply`：O(n + m)，n 为现存条目数（候选复制与淘汰），m 为批次操作数。
- `Expire`：O(n + k log k)，k 为到期条目数（排序输出）。
- `Snapshot` / `Clone`：O(n log n) / O(n)。
- `Stats` / `ValidateBatch`：O(1) / O(m)。
