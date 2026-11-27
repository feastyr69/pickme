import type { Metadata } from "next";
import "./styles.css";

export const metadata: Metadata = { title: "pickME — Event photos together", description: "Your event photos, together." };

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <html lang="en"><body>{children}</body></html>;
}
