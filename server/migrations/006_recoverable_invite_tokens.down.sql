BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM group_invitations
        WHERE token IS NOT NULL AND expires_at > NOW()
    ) THEN
        RAISE EXCEPTION 'Refusing to drop invite tokens while active links exist';
    END IF;
END;
$$;

ALTER TABLE group_invitations
    DROP COLUMN IF EXISTS token;

COMMIT;
