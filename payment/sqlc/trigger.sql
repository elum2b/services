
DROP TRIGGER IF EXISTS payment_order_create_purchase_stats ON payment_order;
DROP TRIGGER IF EXISTS payment_refund_create_stats ON payment_refund;
DROP TRIGGER IF EXISTS payment_refund_update_stats ON payment_refund;
DROP TRIGGER IF EXISTS payment_order_create_daily_stats ON payment_order;
DROP TRIGGER IF EXISTS payment_order_update_daily_stats ON payment_order;
DROP TRIGGER IF EXISTS payment_order_event_update_daily_overview ON payment_stats_order_event;
DROP TRIGGER IF EXISTS payment_stats_event_update_daily_overview ON payment_stats_event;
DROP TRIGGER IF EXISTS payment_stats_event_create_daily_buyer ON payment_stats_event;
DROP TRIGGER IF EXISTS payment_daily_buyer_update_overview ON payment_stats_daily_buyer;

CREATE OR REPLACE FUNCTION payment_order_purchase_stats_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status = 'fulfilled' AND OLD.status <> 'fulfilled' THEN
        INSERT INTO payment_stats_event (
            event_type, source_id, workspace_id, product_id,
            app_id, platform_id, platform_user_id, quantity,
            asset_code, amount_minor, occurred_at
        ) VALUES (
            'purchase', NEW.id, NEW.workspace_id, NEW.product_id,
            NEW.app_id,
            COALESCE(NEW.payer_platform_id, NEW.platform_id),
            COALESCE(NEW.payer_platform_user_id, NEW.platform_user_id),
            NEW.quantity,
            NEW.asset_code,
            NEW.payable_amount_minor,
            COALESCE(NEW.fulfilled_at, now())
        ) ON CONFLICT (event_type, source_id) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_order_create_purchase_stats
AFTER UPDATE ON payment_order
FOR EACH ROW
EXECUTE FUNCTION payment_order_purchase_stats_fn();

CREATE OR REPLACE FUNCTION payment_refund_stats_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status = 'succeeded' AND (TG_OP = 'INSERT' OR OLD.status <> 'succeeded') THEN
        INSERT INTO payment_stats_event (
            event_type, source_id, workspace_id, product_id,
            app_id, platform_id, platform_user_id, quantity,
            asset_code, amount_minor, occurred_at
        )
        SELECT
            'refund', NEW.id, o.workspace_id, o.product_id,
            o.app_id,
            COALESCE(o.payer_platform_id, o.platform_id),
            COALESCE(o.payer_platform_user_id, o.platform_user_id),
            0,
            NEW.asset_code,
            NEW.amount_minor,
            CASE WHEN TG_OP = 'INSERT' THEN NEW.created_at ELSE NEW.updated_at END
        FROM payment_order o
        WHERE o.id = NEW.order_id
        ON CONFLICT (event_type, source_id) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_refund_create_stats
AFTER INSERT ON payment_refund
FOR EACH ROW
EXECUTE FUNCTION payment_refund_stats_fn();

CREATE TRIGGER payment_refund_update_stats
AFTER UPDATE ON payment_refund
FOR EACH ROW
EXECUTE FUNCTION payment_refund_stats_fn();

CREATE OR REPLACE FUNCTION payment_order_daily_stats_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        INSERT INTO payment_stats_order_event (
            order_id, workspace_id, product_id, event_type, order_status, occurred_at
        ) VALUES (
            NEW.id, NEW.workspace_id, NEW.product_id, 'created', (NEW.status::text)::payment_stats_order_event_order_status, NEW.created_at
        ) ON CONFLICT (order_id, event_type, order_status) DO NOTHING;

        INSERT INTO payment_stats_order_event (
            order_id, workspace_id, product_id, event_type, order_status, occurred_at
        ) VALUES (
            NEW.id, NEW.workspace_id, NEW.product_id, 'status', (NEW.status::text)::payment_stats_order_event_order_status, NEW.created_at
        ) ON CONFLICT (order_id, event_type, order_status) DO NOTHING;
    ELSIF NEW.status <> OLD.status THEN
        INSERT INTO payment_stats_order_event (
            order_id, workspace_id, product_id, event_type, order_status, occurred_at
        ) VALUES (
            NEW.id, NEW.workspace_id, NEW.product_id, 'status', (NEW.status::text)::payment_stats_order_event_order_status, NEW.updated_at
        ) ON CONFLICT (order_id, event_type, order_status) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_order_create_daily_stats
AFTER INSERT ON payment_order
FOR EACH ROW
EXECUTE FUNCTION payment_order_daily_stats_fn();

CREATE TRIGGER payment_order_update_daily_stats
AFTER UPDATE ON payment_order
FOR EACH ROW
EXECUTE FUNCTION payment_order_daily_stats_fn();

CREATE OR REPLACE FUNCTION payment_order_event_daily_overview_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO payment_stats_daily_overview (
        workspace_id,
        stats_date,
        orders_created,
        draft_orders,
        pending_payment_orders,
        paid_orders,
        fulfilled_orders,
        canceled_orders,
        expired_orders,
        refunded_orders,
        chargebacked_orders,
        failed_orders
    ) VALUES (
        NEW.workspace_id,
        DATE(NEW.occurred_at),
        CASE WHEN NEW.event_type = 'created' THEN 1 ELSE 0 END,
        CASE WHEN NEW.event_type = 'status' AND NEW.order_status = 'draft' THEN 1 ELSE 0 END,
        CASE WHEN NEW.event_type = 'status' AND NEW.order_status = 'pending_payment' THEN 1 ELSE 0 END,
        CASE WHEN NEW.event_type = 'status' AND NEW.order_status = 'paid' THEN 1 ELSE 0 END,
        CASE WHEN NEW.event_type = 'status' AND NEW.order_status = 'fulfilled' THEN 1 ELSE 0 END,
        CASE WHEN NEW.event_type = 'status' AND NEW.order_status = 'canceled' THEN 1 ELSE 0 END,
        CASE WHEN NEW.event_type = 'status' AND NEW.order_status = 'expired' THEN 1 ELSE 0 END,
        CASE WHEN NEW.event_type = 'status' AND NEW.order_status = 'refunded' THEN 1 ELSE 0 END,
        CASE WHEN NEW.event_type = 'status' AND NEW.order_status = 'chargebacked' THEN 1 ELSE 0 END,
        CASE WHEN NEW.event_type = 'status' AND NEW.order_status = 'failed' THEN 1 ELSE 0 END
    ) ON CONFLICT (workspace_id, stats_date) DO UPDATE SET
        orders_created = payment_stats_daily_overview.orders_created + EXCLUDED.orders_created,
        draft_orders = payment_stats_daily_overview.draft_orders + EXCLUDED.draft_orders,
        pending_payment_orders = payment_stats_daily_overview.pending_payment_orders + EXCLUDED.pending_payment_orders,
        paid_orders = payment_stats_daily_overview.paid_orders + EXCLUDED.paid_orders,
        fulfilled_orders = payment_stats_daily_overview.fulfilled_orders + EXCLUDED.fulfilled_orders,
        canceled_orders = payment_stats_daily_overview.canceled_orders + EXCLUDED.canceled_orders,
        expired_orders = payment_stats_daily_overview.expired_orders + EXCLUDED.expired_orders,
        refunded_orders = payment_stats_daily_overview.refunded_orders + EXCLUDED.refunded_orders,
        chargebacked_orders = payment_stats_daily_overview.chargebacked_orders + EXCLUDED.chargebacked_orders,
        failed_orders = payment_stats_daily_overview.failed_orders + EXCLUDED.failed_orders,
        updated_at = now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_order_event_update_daily_overview
AFTER INSERT ON payment_stats_order_event
FOR EACH ROW
EXECUTE FUNCTION payment_order_event_daily_overview_fn();

CREATE OR REPLACE FUNCTION payment_stats_event_daily_overview_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO payment_stats_daily_overview (
        workspace_id,
        stats_date,
        purchase_count,
        purchase_quantity,
        refund_count
    ) VALUES (
        NEW.workspace_id,
        DATE(NEW.occurred_at),
        CASE WHEN NEW.event_type = 'purchase' THEN 1 ELSE 0 END,
        CASE WHEN NEW.event_type = 'purchase' THEN NEW.quantity ELSE 0 END,
        CASE WHEN NEW.event_type = 'refund' THEN 1 ELSE 0 END
    ) ON CONFLICT (workspace_id, stats_date) DO UPDATE SET
        purchase_count = payment_stats_daily_overview.purchase_count + EXCLUDED.purchase_count,
        purchase_quantity = payment_stats_daily_overview.purchase_quantity + EXCLUDED.purchase_quantity,
        refund_count = payment_stats_daily_overview.refund_count + EXCLUDED.refund_count,
        updated_at = now();

    IF NEW.event_type = 'purchase' THEN
        INSERT INTO payment_stats_daily_buyer (
            workspace_id,
            stats_date,
            app_id,
            platform_id,
            platform_user_id
        ) VALUES (
            NEW.workspace_id,
            DATE(NEW.occurred_at),
            NEW.app_id,
            NEW.platform_id,
            NEW.platform_user_id
        ) ON CONFLICT (workspace_id, stats_date, app_id, platform_id, platform_user_id) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_stats_event_update_daily_overview
AFTER INSERT ON payment_stats_event
FOR EACH ROW
EXECUTE FUNCTION payment_stats_event_daily_overview_fn();

CREATE OR REPLACE FUNCTION payment_daily_buyer_update_overview_fn()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO payment_stats_daily_overview (
        workspace_id,
        stats_date,
        unique_buyers
    ) VALUES (
        NEW.workspace_id,
        NEW.stats_date,
        1
    ) ON CONFLICT (workspace_id, stats_date) DO UPDATE SET
        unique_buyers = payment_stats_daily_overview.unique_buyers + 1,
        updated_at = now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER payment_daily_buyer_update_overview
AFTER INSERT ON payment_stats_daily_buyer
FOR EACH ROW
EXECUTE FUNCTION payment_daily_buyer_update_overview_fn();
