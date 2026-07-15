CREATE TABLE backup_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    automatic_enabled boolean NOT NULL DEFAULT true,
    schedule_preset text NOT NULL DEFAULT 'daily'
        CHECK (schedule_preset IN ('daily', 'weekly', 'advanced')),
    cron_expression text NOT NULL DEFAULT '0 2 * * *',
    timezone text NOT NULL DEFAULT 'UTC',
    retention_count integer NOT NULL DEFAULT 14
        CHECK (retention_count BETWEEN 1 AND 365),
    last_attempt_datetime timestamptz,
    last_success_datetime timestamptz,
    last_failure_datetime timestamptz,
    last_failure_message text,
    updated_datetime timestamptz NOT NULL DEFAULT now()
);

INSERT INTO backup_settings (singleton)
VALUES (true)
ON CONFLICT (singleton) DO NOTHING;
