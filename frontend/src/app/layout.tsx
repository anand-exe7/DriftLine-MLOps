import type { Metadata } from "next";
import { Poppins, JetBrains_Mono } from "next/font/google";
import { Sidebar } from "@/components/Sidebar";
import "./globals.css";

const poppins = Poppins({
  weight: ["300", "400", "500", "600", "700"],
  subsets: ["latin"],
  variable: "--font-poppins",
  display: "swap",
});
const mono = JetBrains_Mono({
  weight: ["400", "500", "600"],
  subsets: ["latin"],
  variable: "--font-mono",
  display: "swap",
});

export const metadata: Metadata = {
  title: "Driftline — MLOps Control Plane",
  description: "Model registry, live canary routing, drift detection and monitoring for Driftline",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className={`${poppins.variable} ${mono.variable} h-full antialiased`} suppressHydrationWarning>
      <head>
        {/* Default to light beige theme */}
        <script
          dangerouslySetInnerHTML={{
            __html: `try{var t=localStorage.getItem("driftline-theme")||"light";document.documentElement.dataset.theme=t}catch(e){document.documentElement.dataset.theme="light";}`,
          }}
        />
      </head>
      <body className="min-h-full font-sans">
        <div className="lg:flex">
          <Sidebar />
          <main className="min-w-0 flex-1 px-4 py-6 sm:px-8 lg:py-8">
            <div className="mx-auto max-w-7xl">{children}</div>
          </main>
        </div>
      </body>
    </html>
  );
}
