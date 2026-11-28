"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { supabase } from "../../../../../lib/supabase";

type Photo = {
  id: string;
  group_id: string;
  thumb_url: string;
  preview_url: string;
  status: string;
  created_at: string;
};

const api = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export default function CollectionGalleryPage() {
  const { id: groupId, collectionId } = useParams<{ id: string; collectionId: string }>();
  const router = useRouter();
  const [photos, setPhotos] = useState<Photo[]>([]);
  const [title, setTitle] = useState("Photo collection");
  const [activePhoto, setActivePhoto] = useState<Photo | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [downloadError, setDownloadError] = useState("");
  const [downloading, setDownloading] = useState(false);
  const closeButtonRef = useRef<HTMLButtonElement>(null);

  const load = useCallback(
    async (signal: AbortSignal) => {
      if (!supabase) throw new Error("Supabase is not configured.");
      const { data, error: sessionError } = await supabase.auth.getSession();
      if (sessionError) throw sessionError;
      const token = data.session?.access_token;
      if (!token) {
        router.replace("/");
        return;
      }
      const suffix =
        collectionId === "no-faces"
          ? "no-face-photos"
          : collectionId === "unprocessed"
            ? "unprocessed-photos"
            : `face-groups/${encodeURIComponent(collectionId)}/photos`;
      const response = await fetch(`${api}/api/groups/${encodeURIComponent(groupId)}/${suffix}`, {
        headers: { Authorization: `Bearer ${token}` },
        signal,
      });
      if (!response.ok)
        throw new Error((await response.text()) || "Could not load this collection.");
      const results = (await response.json()) as Photo[];
      setPhotos(results);
      if (collectionId === "no-faces") setTitle("No faces");
      else if (collectionId === "unprocessed") setTitle("Unprocessed");
      else {
        const overviewResponse = await fetch(
          `${api}/api/groups/${encodeURIComponent(groupId)}/face-groups`,
          {
            headers: { Authorization: `Bearer ${token}` },
            signal,
          },
        );
        if (!overviewResponse.ok)
          throw new Error((await overviewResponse.text()) || "Could not load collection details.");
        const overview = (await overviewResponse.json()) as {
          face_groups: { id: string; label: string }[];
        };
        setTitle(
          overview.face_groups.find((face) => face.id === collectionId)?.label ?? "Face group",
        );
      }
    },
    [collectionId, groupId, router],
  );

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal)
      .catch((loadError: unknown) => {
        if (!controller.signal.aborted)
          setError(
            loadError instanceof Error ? loadError.message : "Could not load this collection.",
          );
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [load]);

  useEffect(() => {
    if (!activePhoto) return;
    const previousFocus = document.activeElement;
    closeButtonRef.current?.focus();
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setActivePhoto(null);
      if (event.key === "Tab") {
        event.preventDefault();
        closeButtonRef.current?.focus();
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
      if (previousFocus instanceof HTMLElement) previousFocus.focus();
    };
  }, [activePhoto]);

  async function downloadOriginal(photo: Photo) {
    setDownloading(true);
    setDownloadError("");
    try {
      const { data, error: sessionError } = await supabase!.auth.getSession();
      if (sessionError) throw sessionError;
      const token = data.session?.access_token;
      if (!token) throw new Error("Sign in to download this photo.");
      const response = await fetch(`${api}/api/photos/${encodeURIComponent(photo.id)}/download`, {
        headers: { Authorization: `Bearer ${token}` },
      });
      if (!response.ok)
        throw new Error((await response.text()) || "Could not create a download link.");
      const { download_url: downloadURL } = (await response.json()) as { download_url: string };
      window.location.assign(downloadURL);
    } catch (downloadFailure) {
      setDownloadError(
        downloadFailure instanceof Error
          ? downloadFailure.message
          : "Could not download this photo.",
      );
    } finally {
      setDownloading(false);
    }
  }

  async function signOut() {
    await supabase?.auth.signOut();
    router.replace("/");
  }

  return (
    <main className="groups-shell">
      <header className="topbar">
        <Link className="brand" href="/groups" aria-label="pickME groups">
          pick<span>ME</span>
        </Link>
        <button className="text-button" onClick={signOut} type="button">
          Sign out
        </button>
      </header>
      <section className="gallery-content" aria-labelledby="gallery-title">
        <Link className="back-link" href={`/groups/${encodeURIComponent(groupId)}`}>
          <span aria-hidden="true">←</span> Back to group
        </Link>
        <div className="gallery-heading collection-heading">
          <div>
            <p className="eyebrow">PHOTO COLLECTION</p>
            <h1 id="gallery-title">{title}</h1>
            {!loading && !error && (
              <p className="subtext">
                {photos.length} {photos.length === 1 ? "photo" : "photos"}
              </p>
            )}
          </div>
        </div>
        {downloadError && (
          <p className="error" role="alert">
            {downloadError}
          </p>
        )}
        {loading ? (
          <p className="state" role="status">
            Loading photos…
          </p>
        ) : error ? (
          <div className="gallery-error" role="alert">
            <h2>Couldn’t load this collection</h2>
            <p>{error}</p>
          </div>
        ) : photos.length === 0 ? (
          <div className="gallery-empty">
            <h2>No photos here yet</h2>
            <p>New uploads will appear once they finish processing.</p>
          </div>
        ) : (
          <div className="photo-gallery" aria-label={`${title} gallery`}>
            {photos.map((photo) => (
              <button
                className="gallery-photo"
                key={photo.id}
                type="button"
                onClick={() => {
                  setActivePhoto(photo);
                  setDownloadError("");
                }}
                aria-label={`View photo added ${new Date(photo.created_at).toLocaleDateString()}`}
              >
                <img
                  src={photo.thumb_url}
                  alt={`Photo added ${new Date(photo.created_at).toLocaleDateString()}`}
                  loading="lazy"
                  decoding="async"
                />
                <span className="gallery-photo-date">
                  {new Date(photo.created_at).toLocaleDateString(undefined, {
                    month: "short",
                    day: "numeric",
                    year: "numeric",
                  })}
                </span>
              </button>
            ))}
          </div>
        )}
      </section>
      {activePhoto && (
        <div className="lightbox-backdrop" onMouseDown={() => setActivePhoto(null)}>
          <section
            className="lightbox"
            role="dialog"
            aria-modal="true"
            aria-label="Photo preview"
            onMouseDown={(event) => event.stopPropagation()}
          >
            <div className="lightbox-toolbar">
              <p>
                {new Date(activePhoto.created_at).toLocaleDateString(undefined, {
                  dateStyle: "long",
                })}
              </p>
              <div className="lightbox-actions">
                <button
                  className="download-button"
                  type="button"
                  disabled={downloading}
                  onClick={() => void downloadOriginal(activePhoto)}
                >
                  {downloading ? "Preparing…" : "Download original"}
                </button>
                <button
                  ref={closeButtonRef}
                  className="lightbox-close"
                  type="button"
                  onClick={() => setActivePhoto(null)}
                >
                  Close <span aria-hidden="true">×</span>
                </button>
              </div>
            </div>
            <img
              className="lightbox-image"
              src={activePhoto.preview_url}
              alt={`Photo added ${new Date(activePhoto.created_at).toLocaleDateString()}`}
            />
          </section>
        </div>
      )}
    </main>
  );
}
