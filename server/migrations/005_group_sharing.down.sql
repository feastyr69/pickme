BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM group_members)
       OR EXISTS (SELECT 1 FROM group_invitations WHERE expires_at > NOW()) THEN
        RAISE EXCEPTION 'Refusing to remove group sharing data; export or remove memberships and invitations first';
    END IF;
END;
$$;

DROP TABLE IF EXISTS group_invitations;
DROP TABLE IF EXISTS group_members;

COMMIT;
