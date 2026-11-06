DROP TABLE user_recovery_codes, system_alert_states, system_alert_subscriptions, user_passkeys, auth_challenges, user_totp, dashboard_panels, notification_deliveries;
DELETE FROM notification_channels WHERE type <> 'webhook';
ALTER TABLE notification_channels DROP CONSTRAINT notification_channels_type_check;
ALTER TABLE notification_channels ADD CONSTRAINT notification_channels_type_check CHECK (type IN ('webhook'));
