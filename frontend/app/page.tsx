"use client";

import { useState } from "react";
import { supabase } from "../lib/supabase";

export default function SignInPage() {
  const [error, setError] = useState("");
  async function signIn() {
    if (!supabase) { setError("Add your Supabase URL and anon key to web/.env.local."); return; }
    const { error } = await supabase.auth.signInWithOAuth({ provider: "google", options: { redirectTo: `${location.origin}/groups` } });
    if (error) setError(error.message);
  }
  return <main className="auth-shell"><section className="auth-card"><a className="brand" href="/">pick<span>ME</span></a><p className="eyebrow">EVERY MOMENT, SHARED</p><h1>Your event.<br/>Every point of view.</h1><p className="subtext">Bring everyone’s photos together in one place.</p><button className="google-button" onClick={signIn}><span aria-hidden="true">G</span> Continue with Google</button>{error && <p className="error" role="alert">{error}</p>}<p className="fine-print">Sign in to create a group or see the moments you’ve shared.</p></section><div className="photo-panel" aria-label="Colorful memory collage"><div className="photo photo-one"/><div className="photo photo-two"/><div className="photo photo-three"/><div className="photo-note">good times, together <span>✳</span></div></div></main>;
}
