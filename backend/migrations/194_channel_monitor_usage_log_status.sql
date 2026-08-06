-- 渠道监控支持基于真实请求日志的状态判定。
-- account_id / channel_id 用于把监控与具体账号/渠道关联；
-- 当关联存在且最近有请求日志时，优先按日志计算状态，否则回退到探测。
ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS account_id BIGINT,
    ADD COLUMN IF NOT EXISTS channel_id BIGINT,
    ADD COLUMN IF NOT EXISTS use_logs_for_status BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS status_source VARCHAR(20) NOT NULL DEFAULT 'probe';

-- 关联账号/渠道后，便于按 account_id / channel_id 查询 usage_logs
CREATE INDEX IF NOT EXISTS idx_channel_monitors_account_id ON channel_monitors(account_id);
CREATE INDEX IF NOT EXISTS idx_channel_monitors_channel_id ON channel_monitors(channel_id);
