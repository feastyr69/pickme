"use client";

import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { supabase } from "../../../lib/supabase";

type InviteDetails = { group_id: string; group_name: string; expires_at: string };

const api = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export default function InvitePage() {
  const { token } = useParams<{ token: string }>();
  const router = useRouter();
  const [details, setDetails] = useState<InviteDetails | null>(null);
  const [signedIn, setSignedIn] = useState(false);
  const [loading, setLoading] = useState(true);
  const [accepting, setAccepting] = useState(false);
  const [error, setError] = useState("");

  const loadInvite = useCallback(async () => {
    if (!supabase) throw new Error("Supabase is not configured.");
    const { data, error: sessionError } = await supabase.auth.getSession();
    if (sessionError) throw sessionError;
    const accessToken = data.session?.access_token;
    if (!accessToken) {
      setSignedIn(false);
      return;
    }

    setSignedIn(true);
    const response = await fetch(`${api}/api/invites/${encodeURIComponent(token)}`, {
      headers: { Authorization: `Bearer ${accessToken}` },
    });
    if (!response.ok)
      throw new Error((await response.text()) || "This invite link is invalid or expired.");
    setDetails((await response.json()) as InviteDetails);
  }, [token]);

  useEffect(() => {
    void loadInvite()
      .catch((loadError: unknown) => {
        setError(
          loadError instanceof Error ? loadError.message : "Could not load this invitation.",
        );
      })
      .finally(() => setLoading(false));
  }, [loadInvite]);

  async function acceptInvite() {
    if (!supabase) {
      setError("Supabase is not configured.");
      return;
    }
    setAccepting(true);
    setError("");
    try {
      const { data, error: sessionError } = await supabase.auth.getSession();
      if (sessionError) throw sessionError;
      const accessToken = data.session?.access_token;
      if (!accessToken) throw new Error("Sign in before accepting this invitation.");
      const response = await fetch(`${api}/api/invites/${encodeURIComponent(token)}/accept`, {
        method: "POST",
        headers: { Authorization: `Bearer ${accessToken}` },
      });
      if (!response.ok)
        throw new Error((await response.text()) || "Could not accept this invitation.");
      const result = (await response.json()) as { group_id: string };
      router.replace(`/groups/${encodeURIComponent(result.group_id)}`);
    } catch (acceptError) {
      setError(
        acceptError instanceof Error ? acceptError.message : "Could not accept this invitation.",
      );
      setAccepting(false);
    }
  }

  const signInHref = `/?next=${encodeURIComponent(`/invite/${token}`)}`;

  return (
    <main className="groups-shell">
      <header className="topbar">
        <Link className="brand" href="/groups" aria-label="pickME groups">
          pick<span>ME</span>
        </Link>
      </header>
      <section className="gallery-content invite-content" aria-labelledby="invite-title">
        {loading ? (
          <p className="state" role="status">
            Loading invitation…
          </p>
        ) : error && !details ? (
          <div className="gallery-error" role="alert">
            <h1 id="invite-title">Invitation unavailable</h1>
            <p>{error}</p>
            <Link className="back-link" href="/groups">
              Go to groups
            </Link>
          </div>
        ) : !signedIn ? (
          <div className="gallery-empty">
            <p className="eyebrow">GROUP INVITATION</p>
            <h1 id="invite-title">Sign in to join</h1>
            <p>Sign in with your account, then return here to accept the invitation.</p>
            <Link className="primary-button invite-sign-in" href={signInHref}>
              Continue to sign in
            </Link>
          </div>
        ) : details ? (
          <div className="gallery-empty">
            <p className="eyebrow">GROUP INVITATION</p>
            <h1 id="invite-title">Join {details.group_name}</h1>
            <p>
              Anyone with this link can join until it expires on{" "}
              {new Date(details.expires_at).toLocaleString()}. The link can be used by multiple
              people.
            </p>
            {error && (
              <p className="error" role="alert">
                {error}
              </p>
            )}
            <button
              className="primary-button"
              type="button"
              disabled={accepting}
              onClick={() => void acceptInvite()}
            >
              {accepting ? "Joining group…" : "Accept invitation"}
            </button>
          </div>
        ) : null}
      </section>
    </main>
  );
}
