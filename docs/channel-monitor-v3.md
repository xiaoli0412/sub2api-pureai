# 渠道状态监控 V3：来源、原理与移植说明

> 本文说明 `git1hu1bou111/hakimi-sub2api` 中自研的「V3 渠道状态台」是什么、它改了什么、
> 为什么这样改，以及它是如何被移植进本仓库（上游 `Wei-Shaw/sub2api` v0.2.8）的。
>
> 上游基线：`a3eb7ef30`（v0.2.8，2026-09-23）
> 参考仓库：`git1hu1bou111/hakimi-sub2api` `main` @ `bf8d9a56e`（基于上游 v0.1.183，2026-08-30）

---

## 1. 一句话概括

**V3 不是第三套采集系统，而是长在 V2 被动聚合管道之上的「按分组展示层」。**

它复用 V2 的全部读接口（`/channel-monitor-v2/*`），只做两件事：

1. 换一套**分组维度**的呈现方式（一个渠道分组一张卡片 + 可用率时间轴）；
2. 收紧**什么算一次有效样本**的口径，让渠道之间可以公平对比。

理解这一点是理解后面所有设计的前提：V3 的数据来自 V2，没有独立的数据采集。

---

## 2. 三套模式的关系

| | V1 | V2 | V3 |
|---|---|---|---|
| 数据来源 | 主动探测（向上游发真实请求） | 被动聚合真实流量 | 同 V2 |
| 触发方式 | 定时任务 | 后台聚合器 | 同 V2 |
| 是否产生探测流量 | **是** | 否 | 否 |
| 数据表 | `channel_monitors` / `channel_monitor_histories` | `channel_monitor_v2_*` | 同 V2 |
| 读接口 | `/channel-monitors/*` | `/channel-monitor-v2/*` | 同 V2 |
| 前端入口 | `ChannelStatusV1View` | `ChannelStatusV2View` | `ChannelStatusV3View` |
| 后端模式值 | `v1` | `v2` | `v3` |

三者在后端由 **一个独占开关** `channel_monitor_mode` 控制（`v1` / `v2` / `v3`）。

关键实现点：V3 与 V2 共用 `ChannelMonitorRuntime.PassiveAggregationAllowed()` 这一个谓词。
它同时把关三处——用户端路由、管理端路由、后台聚合器。所以把 `v3` 纳入这个谓词，
就等于一次性把 V3 接进了整条管道；而 `ActiveProbesAllowed()` 仍然只认 `v1`，
因此 V3 模式下 V1 探测保持退役，不会偷偷发探测流量。

```
                       ┌─ ActiveProbesAllowed()  → 仅 v1    → V1 探测
channel_monitor_mode ──┤
                       └─ PassiveAggregationAllowed() → v2/v3 ─┬→ 聚合器运行
                                                               ├→ /channel-monitor-v2 读接口开放
                                                               ├→ V2 视图
                                                               └→ V3 视图
```

---

## 3. 数据流

```
usage_logs ─────────┐
                    ├─► 分钟聚合（写入侧，受「采样口径」约束）
ops_error_logs ─────┘        │
                             ▼
              channel_monitor_v2_metrics_1m          （1 分钟事实表）
              channel_monitor_v2_user_metrics_1m
              channel_monitor_v2_latency_histograms_1m
                             │
                             ▼  分层保留 + 滚动汇总
              5m / 1h / 12h / 1d rollup 表
                             │
                             ▼  只读查询（读侧）
              GET /channel-monitor-v2/{snapshot,matrix,dimensions,models,errors,users}
                             │
                ┌────────────┴────────────┐
                ▼                         ▼
        V2 视图（平台/模型维度）    V3 视图（group_by=platform_group）
```

V3 只额外用到 `group_by=platform_group`：后端会把它**限定在运营配置的 group_ids 内**，
普通用户还会再与自己的分组权限取交集（`channelMonitorV2ScopedGroupIDs` 三方交集：
运营配置 ∩ 查看者权限 ∩ 请求筛选）。因此 V3 页面上不会出现「不属于你」的分组。

---

## 4. 他的核心改动一：采样口径（最重要的部分）

### 4.1 用量侧：只统计「有实质内容的流式文本流量」

```sql
-- channelMonitorV2UsageSampleFilterUL
ul.actual_cost > 0                                  -- 成功计费（原有）
AND ul.stream IS TRUE                                -- 必须流式
AND COALESCE(ul.request_type, 0) NOT IN (4, 6)       -- 排除非对话类型
AND COALESCE(ul.billing_mode, 'token') <> 'image'    -- 排除图片计费
AND COALESCE(ul.image_count, 0) = 0                  -- 排除图片请求
AND COALESCE(ul.image_input_tokens, 0) = 0
AND COALESCE(ul.image_output_tokens, 0) = 0
AND COALESCE(ul.input_tokens, 0)
    + COALESCE(ul.cache_creation_tokens, 0)
    + COALESCE(ul.cache_read_tokens, 0) > 10000      -- 只留「大请求」
```

**为什么这么改（核心洞察）：**

上游原来的口径是「所有成功计费的请求」。但一次 20 token 的同步小调用和一次 50k token 的
流式长会话，它们的**首 Token 延迟、缓存命中率、失败模式完全不同**。混在一起统计时，
一个渠道的指标好坏，很大程度上取决于它的**流量结构**，而不是它的**质量**。
换句话说：原来的数字在渠道之间不可比。

加上这层过滤后，「可用率 / 缓存率 / 首 Token」才真正反映渠道本身的表现。
`> 10000` 这个阈值的作用是确保样本大到足以让缓存行为和 TTFT 有意义。

**代价（必须知道）：** 这个过滤器改的是**共享的 V2 汇总表写入逻辑**，
所以 **V2 页面的数字会同步变化**——小请求、同步请求、图片请求被排除后，
成功率和延迟数字都会移动。这是有意为之的取舍：V2 与 V3 保持同一套「样本定义」，
避免两个页面给出互相矛盾的数字。子 10k 请求的失败**不再计入可用率**，
这是这套口径最需要注意的行为变化。

### 4.2 错误侧：刻意放宽，且必须放宽

```sql
-- channelMonitorV2ErrorSampleFilter
current_error.stream IS TRUE
AND COALESCE(current_error.request_type, 0) NOT IN (4, 6)
AND COALESCE(current_error.inbound_endpoint, current_error.request_path, '') NOT LIKE '/v1/images%'
```

**错误行不携带 token 数**，所以 4.1 里那条 `> 10000` 规则在物理上无法应用。
这里只保留了错误表**能证明**的条件（流式、非 WebSocket、非图片端点）。

这是一个反向的克制：如果错误侧也强行套上更严的条件，真实的上游故障会被**静默隐藏**，
成功率会虚高——而监控存在的意义恰恰是暴露故障。所以宁可放宽错误侧。

---

## 5. 他的核心改动二：分组作用域聚合

> ⚠️ **这部分已被上游 v0.2.8 吸收**，本仓库无需移植，此处仅作原理说明。

- `ChannelMonitorV2Filter` 增加 `AllowedGroupIDs []int64` + `RestrictGroups bool`
- handler 增加 `scopeFilter()`：普通查看者的分组范围由**服务端**从 `GetAvailableGroups(userID)` 推导，
  绝不信任客户端传参；管理员保留完整范围；**授权依赖缺失时 fail-closed**（报错而不是放行全量）
- `channelMonitorV2ScopedGroupIDs()` 做三方交集，并用第二个返回值区分
  「被限制且结果为空」与「管理员无配置 = 全部」

这里面最值得学的是 **fail-closed** 的设计：`apiKeyService == nil` 时不是「跳过限制」，
而是直接 500 拒绝服务。权限组件缺失绝不能被解释成「拥有全部权限」。

---

## 6. 他的核心改动三：V3 前端

| 文件 | 作用 |
|---|---|
| `views/user/ChannelStatusV3View.vue` | 页面：区间切换（90m/24h/7d/30d）、自动刷新与倒计时、卡片网格 |
| `components/user/monitor/ChannelMonitorV3Card.vue` | 单分组卡片：缓存率 / 可用率 / 首 Token + 状态徽章 + 用户倍率 |
| `components/user/monitor/ChannelMonitorV3Timeline.vue` | 可用率时间轴：软玻璃柱、悬停联动、视口夹取 tooltip |
| `i18n/locales/{zh,en}/channelMonitorV3.ts` | 文案 |
| `features/channel-monitor-v2/monitorFormat.ts` | 新增可用率配色助手 |

几个值得注意的实现细节：

1. **卡片显示「最新完成的桶」，不是区间聚合。**
   `latestBucket` 取 `buckets` 中时间最大的一个。语义是「这个渠道**现在**怎么样」，
   而区间聚合回答的是「这段时间平均怎么样」。区间选择只驱动下方时间轴。
2. **时间轴的命中区与视觉层分离。**
   缩放/位移只作用于 `.v3-bar-visual`，`.v3-bar-hitbox` 的几何尺寸永远不变。
   否则柱子放大后会把鼠标「推」到相邻柱子上，产生抖动。
3. **tooltip 按视口夹取。**
   靠近左右边缘时切换对齐方式（`-100%` / `0%` / `-50%`），并 `Teleport to body` 避免被卡片裁剪。
4. **`prefers-reduced-motion` 下关闭动画**，无障碍可用。
5. **可选依赖失败不阻塞监控。**
   用户倍率来自计费接口，失败时只 `console.warn` 并显示 `-`，不影响监控主体。

---

## 7. 移植记录：原样 / 已存在 / 已优化

### 7.1 原样移植

- V3 前端三件套 + i18n + 时间轴动效与 CSS
- 两个采样过滤器（`channelMonitorV2UsageSampleFilterUL` / `channelMonitorV2ErrorSampleFilter`）及其全部调用点

### 7.2 上游已存在，无需移植

移植前逐行核对发现，他 V3 提交里的**后端部分已经被上游 v0.2.8 吸收**，且上游版本更完善：

| 能力 | 本地位置 |
|---|---|
| `scopeFilter` 服务端分组作用域 | `handler/channel_monitor_v2_handler.go:193` |
| `RestrictGroups` / `AllowedGroupIDs` | `service/channel_monitor_v2.go:83` |
| `channelMonitorV2ScopedGroupIDs` 三方交集 | `repository/channel_monitor_v2_repo.go:607` |
| composite 分组→真实账号平台解析 | `repository/channel_monitor_v2_aggregation.go:252` |

上游的改进：把授权依赖抽成 `channelMonitorV2GroupAuthorizer` 接口，测试可注入 stub；
他那边是具体类型 `*service.APIKeyService`，测试只能传 `nil` 硬测。

### 7.3 本次做的优化（相对他的原版）

1. **配色阶梯去重。**
   他写了三套并行的 `if` 阶梯（badge / bar / text），阈值 `30/50/60/80/90` 各重复三遍，
   改一个阈值要同步改三处。现改为**一张有序阈值表** + 三个取用函数，
   阈值只定义一次，三处显示不可能不一致。输出类名与原版**逐字节一致**，视觉零变化。
2. **V3 接入为独立模式而非寄生在 v2 下。**
   他的做法是「`mode=v2` 时默认渲染 V3，V2 靠 `?monitor_view=v2` 回退」——
   V2 要看得到必须手敲 URL 参数。现改为独立 `v3` 模式，三套视图各自独立、后台可切换。
3. **测试锁定。**
   新增 `channel_monitor_v2_sample_filter_test.go`：逐条断言采样口径的每个谓词，
   防止后续改动悄悄把某条规则回退；并断言用量过滤器不得引用 `current_error`、
   错误过滤器不得引用 `ul.`（别名串用会编译通过但永不匹配任何行）。
4. **`MonitorSettingsPanel` 的被动模式提示。**
   原来用 `isChannelMonitorV2Mode()` 判断「聚合是否在跑」，V3 下会误报「聚合不会运行」。
   新增 `isChannelMonitorPassiveMode()`（V2 ∪ V3）并修正提示文案。
5. **i18n 文案修正。**
   原 V3 副标题是「V2 被动用量 · 缓存率与可用率」，在 V3 成为独立模式后已不准确，
   改为「被动聚合 · 仅统计流式文本流量」，直接说明数据口径。

---

## 8. 本仓库的改动清单

**后端**

| 文件 | 改动 |
|---|---|
| `internal/service/domain_constants.go` | 新增 `ChannelMonitorModeV3 = "v3"` |
| `internal/service/setting_public.go` | `normalizeChannelMonitorMode` 接受 `v3`；`PassiveAggregationAllowed()` 纳入 `v3` |
| `internal/server/routes/{admin,user}.go` | 门禁注释更新（复用 `channelMonitorModeV2Guard`，不改名以保留上游合并亲和性） |
| `internal/repository/channel_monitor_v2_aggregation.go` | 新增两个采样过滤器常量并接入 3 处 rollup SQL + 错误聚合 |
| `internal/service/channel_monitor_mode_v3_test.go` | **新增**：v3 模式归一化、聚合放行、探测退役 |
| `internal/repository/channel_monitor_v2_sample_filter_test.go` | **新增**：采样口径锁定 |

**前端**

| 文件 | 改动 |
|---|---|
| `utils/featureFlags.ts` | `ChannelMonitorMode` 加 `v3`；新增 `isChannelMonitorV3Mode` / `isChannelMonitorPassiveMode` |
| `types/index.ts`、`api/admin/settings.ts` | `channel_monitor_mode` 类型加 `v3` |
| `views/admin/SettingsView.vue` | 模式选择器加 V3；被动设置块对 V2/V3 共用；加载/保存映射支持 v3 |
| `i18n/locales/{zh,en}/admin/settings.ts` | `modeV3` / `modeV3Hint`，`modeHint` 更新 |
| `i18n/locales/{zh,en}/channelMonitorV2.ts` | `modeV3`；被动模式文案 |
| `features/channel-monitor-v2/monitorFormat.ts` | 可用率配色阶梯（单表 + 三个取用函数） |
| `features/channel-monitor-v2/MonitorSettingsPanel.vue` | 改用 `isChannelMonitorPassiveMode()` |
| `views/user/ChannelStatusView.vue` | 三路视图分发 |
| `views/user/ChannelStatusV3View.vue` | **新增** |
| `components/user/monitor/ChannelMonitorV3Card.vue` | **新增** |
| `components/user/monitor/ChannelMonitorV3Timeline.vue` | **新增** |
| `i18n/locales/{zh,en}/channelMonitorV3.ts` + 两个 `index.ts` | **新增**并注册 |

---

## 9. 如何启用与验证

**启用：** 后台「系统设置 → 功能开关 → 渠道监控」，模式选 **V3 分组状态台**。
或直接改 DB：

```sql
UPDATE settings SET value = 'v3' WHERE key = 'channel_monitor_mode';
```

前端「渠道状态」页面即渲染 V3；切回 `v2` / `v1` 分别回到原视图。

**后端验证：**

```bash
cd backend
go build ./...
go test -tags unit -run 'V3|SampleFilter|RollupsGate|ChannelMonitor' ./internal/service/ ./internal/repository/
```

**前端验证：**

```bash
cd frontend
pnpm install --frozen-lockfile   # 注意：本仓库 lockfile 为 v9.0，请用 pnpm 9/10（pnpm 11 不再读取 package.json 的 pnpm.overrides）
pnpm run typecheck
pnpm run test:run
pnpm run build
```

### 9.1 本次移植的实测结果

| 检查项 | 结果 |
|---|---|
| `go build ./...` | ✅ 通过 |
| 后端监控相关单测（`V3` / `SampleFilter` / `RollupsGate` / `ChannelMonitor`） | ✅ 全部通过（含 6 个 v3 模式用例 + 5 个采样口径用例） |
| 前端 `vue-tsc --noEmit` | ✅ 无错误 |
| 前端全量测试 | ✅ 336 个文件 / 2503 个用例全部通过 |
| 前端 `vite build` | ✅ 构建成功 |
| i18n 键完整性（`check:i18n`） | ✅ zh/en 键一致 |

**两个与本次改动无关的既存失败**（已在纯净上游 `a3eb7ef30` 上复现，确认不是本次引入）：

| 失败项 | 说明 |
|---|---|
| `internal/service` → `TestOllamaProbeCallback_StaleLongDoesNotOverrideNewShort` | Ollama 探测回调 CAS 断言失败，与渠道监控无关 |
| `internal/util/responseheaders` | 该包测试失败，与渠道监控无关 |

> 前端测试环境注意：`vitest.config.ts` 把 `vue-i18n` 别名到 **runtime-only** 构建，
> 因此测试里没有消息编译器，直接 `createI18n({ messages })` 时 `t()` 会原样返回 key。
> 需要断言真实文案时，请在 spec 中 mock `useI18n` 并对真实 locale 模块做点号路径解析
> （参见 `components/user/monitor/__tests__/ChannelMonitorV3Card.spec.ts`）。

---

## 10. 已知限制与风险

1. **采样阈值需要按实际流量校准。** `> 10000` token 是经验值。若你的用户以小请求为主，
   V3 卡片会大量显示「样本不足」。建议上线后按实际分布调整阈值
   （`channelMonitorV2UsageSampleFilterUL`，单点修改）。
2. **历史数据不会自动重算。** 采样口径变化只影响**之后**写入的聚合结果；
   已有 rollup 仍是旧口径。若要统一，需要触发对应时间窗的重算/回填。
3. **子 10k 请求的失败不计入可用率。** 见 4.1 的取舍说明。
4. **读侧未做同口径过滤。** 过滤发生在 rollup 写入侧，读侧直接消费已过滤的计数。
   若后续新增直接查 `usage_logs` 的读路径，必须复用同一个过滤器，
   否则会出现「同一页面两套口径」。
5. **V1 稳定性修复未移植。** 他另有 3 处 V1 探测稳定性改动
   （502/503/504 重试 3 次 + 退避、连续 3 次硬失败才判定不可用、聚合器超时与日志）。
   这些会改变 V1 行为，与本仓库「保留原 V1」的目标冲突，故未纳入，详见 `channel-monitor-v3-roadmap.md`。
