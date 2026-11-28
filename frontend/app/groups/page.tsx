"use client";

import { FormEvent, useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { supabase } from "../../lib/supabase";

type Group = { id: string; name: string; created_at: string };
const api = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export default function GroupsPage() {
  const router = useRouter();
  const [token, setToken] = useState("");
  const [groups, setGroups] = useState<Group[]>([]);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    if (!supabase) {
      setError("Supabase is not configured.");
      setLoading(false);
      return;
    }
    supabase.auth.getSession().then(({ data }) => {
      const accessToken = data.session?.access_token;
      if (!accessToken) {
        router.replace("/");
        return;
      }
      setToken(accessToken);
      fetch(`${api}/api/groups`, { headers: { Authorization: `Bearer ${accessToken}` } })
        .then(async (response) => {
          if (!response.ok) throw new Error(await response.text());
          setGroups(await response.json());
        })
        .catch((err) => setError(err.message))
        .finally(() => setLoading(false));
    });
  }, [router]);
  async function createGroup(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const response = await fetch(`${api}/api/groups`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json" },
        body: JSON.stringify({ name }),
      });
      if (!response.ok) throw new Error(await response.text());
      const group = await response.json();
      setGroups((current) => [group, ...current]);
      setName("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not create group");
    }
  }
  async function signOut() {
    await supabase?.auth.signOut();
    router.replace("/");
  }
  return (
    <main className="groups-shell">
      <header className="topbar">
        <a className="brand" href="/">
          pick<span>ME</span>
        </a>
        <button className="text-button" onClick={signOut}>
          Sign out
        </button>
      </header>
      <section className="groups-content">
        <p className="eyebrow">YOUR PEOPLE, YOUR PHOTOS</p>
        <h1>Groups</h1>
        <p className="subtext">Make a space for the moments you want to keep.</p>
        <form className="create-form" onSubmit={createGroup}>
          <label htmlFor="group-name">Create a group</label>
          <div className="form-row">
            <input
              id="group-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Summer weekend"
              required
              maxLength={100}
            />
            <button className="primary-button" type="submit">
              Create group <span aria-hidden="true">＋</span>
            </button>
          </div>
        </form>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        {loading ? (
          <p className="state">Loading your groups…</p>
        ) : groups.length === 0 ? (
          <div className="empty">
            <span className="empty-mark">✳</span>
            <h2>Your first group starts here</h2>
            <p>Create a group and invite everyone to share their photos.</p>
          </div>
        ) : (
          <div className="group-grid">
            {groups.map((group) => (
              <Link className="group-card" href={`/groups/${group.id}`} key={group.id}>
                <span className="group-mark">✳</span>
                <h2>{group.name}</h2>
                <p>Created {new Date(group.created_at).toLocaleDateString()}</p>
                <span className="group-card-action">
                  Open group <span aria-hidden="true">→</span>
                </span>
              </Link>
            ))}
          </div>
        )}
      </section>
    </main>
  );
}
