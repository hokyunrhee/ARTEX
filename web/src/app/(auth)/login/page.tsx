"use client";

import { useEffect, useRef, useState } from "react";

import Image from "next/image";
import { useRouter } from "next/navigation";

import { AlertTriangle, ShieldCheck } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogClose, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import { auth } from "@/lib/auth";

export default function LoginPage() {
  const router = useRouter();
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [checking, setChecking] = useState(true);
  const [agreed, setAgreed] = useState(false);
  const [termsOpen, setTermsOpen] = useState(false);
  const [readToEnd, setReadToEnd] = useState(false);
  const termsBodyRef = useRef<HTMLDivElement>(null);

  // Enable agreement only after reaching the end of the terms, including content that fits without scrolling.
  function handleTermsScroll() {
    const el = termsBodyRef.current;
    if (!el) return;
    if (el.scrollTop + el.clientHeight >= el.scrollHeight - 8) setReadToEnd(true);
  }

  useEffect(() => {
    if (!termsOpen) return;
    // Reset on open, accounting for content that fits in the viewport and cannot trigger scrolling.
    setReadToEnd(false);
    const el = termsBodyRef.current;
    if (el && el.scrollHeight <= el.clientHeight + 8) setReadToEnd(true);
  }, [termsOpen]);

  useEffect(() => {
    // Send authenticated users to the main interface; static exports have no middleware to redirect them.
    const token = auth.getToken();
    if (token) {
      // localStorage may still contain credentials after the cookie is lost. Sync it before making a fresh request
      // so the server guard or route cache does not return to a login page still in the checking state.
      auth.setToken(token);
      window.location.replace("/function/tasks");
      return;
    }
    api
      .authStatus()
      .then(({ initialized }) => {
        if (!initialized) router.replace("/setup");
      })
      .catch(() => setError("Cannot connect to the backend service"))
      .finally(() => setChecking(false));
  }, [router]);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!agreed) {
      setError("Read and accept the Terms of use first");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const { token } = await api.login("ARTEX", password);
      auth.setToken(token);
      window.location.replace("/function/tasks");
    } catch {
      setError("Incorrect username or password");
    } finally {
      setLoading(false);
    }
  }

  if (checking) {
    return (
      <div role="status" className="flex min-h-dvh items-center justify-center text-muted-foreground">
        Checking login status...
      </div>
    );
  }

  return (
    <div className="flex h-dvh">
      {/* Left panel */}
      <div className="hidden flex-col items-center justify-center bg-primary p-12 text-center lg:flex lg:w-1/3">
        <div className="relative flex items-center justify-center">
          <div className="absolute size-80 rounded-full border border-primary-foreground/10" />
          <div className="absolute size-60 rounded-full border border-primary-foreground/15" />
          <div className="absolute size-40 rounded-full border border-primary-foreground/20" />
          <Image
            src="/logo.png"
            alt="ARTEX"
            width={160}
            height={160}
            unoptimized
            loading="eager"
            className="relative brightness-0 invert"
          />
        </div>
      </div>

      {/* Right panel */}
      <div className="flex w-full items-center justify-center bg-background p-8 lg:w-2/3">
        <div className="w-full max-w-md space-y-10 py-24 lg:py-32">
          <div className="space-y-4 text-center">
            <h2 className="font-medium text-2xl tracking-tight">Sign in</h2>
            <p className="mx-auto max-w-xl text-muted-foreground">
              Welcome back. Enter your password to continue using ARTEX.
            </p>
          </div>
          <form onSubmit={handleSubmit} className="flex flex-col gap-4">
            <div className="space-y-1.5">
              <Label htmlFor="username">Username</Label>
              <Input id="username" value="ARTEX" readOnly className="bg-muted text-muted-foreground" />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="Enter your password"
                autoFocus
                autoComplete="current-password"
              />
            </div>
            <div className="flex items-start gap-2">
              <Checkbox
                id="agree-terms"
                checked={agreed}
                onCheckedChange={(v) => setAgreed(v === true)}
                className="mt-0.5"
              />
              <Label htmlFor="agree-terms" className="font-normal text-muted-foreground text-sm leading-relaxed">
                I have read and agree to
                <button
                  type="button"
                  onClick={() => setTermsOpen(true)}
                  className="mx-0.5 font-medium text-primary underline-offset-4 hover:underline"
                >
                  the Terms of use
                </button>
              </Label>
            </div>
            {error && <p className="text-destructive text-sm">{error}</p>}
            <Button type="submit" className="w-full" disabled={loading || !password || !agreed}>
              {loading ? "Signing in..." : "Sign in"}
            </Button>
          </form>
        </div>
      </div>

      <Dialog open={termsOpen} onOpenChange={setTermsOpen}>
        <DialogContent className="gap-0 p-0 sm:max-w-2xl">
          <DialogHeader className="flex-row items-center gap-3 border-b px-6 py-4">
            <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <ShieldCheck className="size-5" />
            </div>
            <div className="space-y-0.5">
              <DialogTitle className="text-base">ARTEX terms of use and disclaimer</DialogTitle>
              <p className="text-muted-foreground text-xs">
                Version v1.0 | Effective September 18, 2026 | Read all terms below before signing in.
              </p>
            </div>
          </DialogHeader>

          <div
            ref={termsBodyRef}
            onScroll={handleTermsScroll}
            className="max-h-[60vh] space-y-5 overflow-y-auto px-6 py-5 text-muted-foreground text-sm leading-relaxed"
          >
            <p className="rounded-lg border bg-muted/40 p-3 text-foreground/80">
              These Terms of use and disclaimer (the "Statement") form an agreement between you and the ARTEX project
              authors and contributors regarding your use of this software. Before using it, carefully read and fully
              understand every provision, especially the disclaimers, limitations of liability, and prohibitions
              highlighted in bold or colored blocks.
              <span className="font-medium text-foreground">
                {" "}
                By downloading, installing, accessing, or otherwise using this software, you acknowledge that you have
                read, understood, and agreed to be bound by this Statement in its entirety.
              </span>
            </p>

            <section className="space-y-1.5">
              <h4 className="flex items-center gap-2 font-medium text-foreground">
                <span className="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">
                  1
                </span>
                Article 1 | Definitions and open-source license
              </h4>
              <p className="pl-7">
                This software (ARTEX) is an open-source program released under the GNU Affero General Public License
                v3.0 (AGPL-3.0). You may freely use, copy, modify, and distribute this software under that license;
                however, any derivative works, including online services provided to third parties over a network, must
                also be released under AGPL-3.0, with the complete corresponding source code made available to users.
                The accompanying LICENSE file contains the authoritative full terms of AGPL-3.0.
              </p>
            </section>

            <section className="space-y-1.5">
              <h4 className="flex items-center gap-2 font-medium text-foreground">
                <span className="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">
                  2
                </span>
                Article 2 | Permitted use
              </h4>
              <p className="pl-7">
                This software is intended solely for personal learning, code research, studying security principles, and
                technical validation in a local, isolated environment that you set up yourself. Permitted uses are
                nonoffensive and nondestructive, such as learning, academic research, and code review. You may not use
                this software for any purpose other than those expressly permitted in this article.
              </p>
            </section>

            <section className="space-y-2">
              <h4 className="flex items-center gap-2 font-medium text-destructive">
                <span className="flex size-5 items-center justify-center rounded-md bg-destructive/10 font-semibold text-destructive text-xs">
                  3
                </span>
                <AlertTriangle className="size-4" />
                Article 3 | Prohibited activities
              </h4>
              <ul className="ml-7 list-decimal space-y-1.5 rounded-lg border border-destructive/20 bg-destructive/5 p-3 pl-8 text-foreground/80 marker:text-destructive/70">
                <li>
                  Scanning, probing, exploiting, or attacking any website, online service, or networked system owned by
                  others or third parties is strictly prohibited, regardless of authorization or whether the assets
                  belong to you;
                </li>
                <li>
                  Using this software for actual penetration testing, offensive or defensive engagements, red-team or
                  blue-team exercises, or production environments is strictly prohibited;
                </li>
                <li>
                  Using this software for unauthorized intrusion, data theft, extortion, denial of service (DoS/DDoS),
                  or any destructive or criminal activity is strictly prohibited;
                </li>
                <li>
                  Removing, altering, or circumventing any copyright, license, or safety notice in this software or its
                  output is strictly prohibited;
                </li>
                <li>
                  Any activity that violates the laws, regulations, or regulatory requirements of your country or region
                  is strictly prohibited.
                </li>
              </ul>
            </section>

            <section className="space-y-1.5">
              <h4 className="flex items-center gap-2 font-medium text-foreground">
                <span className="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">
                  4
                </span>
                Article 4 | Intellectual property
              </h4>
              <p className="pl-7">
                Copyright and related intellectual property rights in this software belong to the project authors and
                contributors. You receive the rights granted under AGPL-3.0. Other than the rights expressly granted by
                that license, this Statement grants no additional rights, whether expressly or by implication.
              </p>
            </section>

            <section className="space-y-1.5">
              <h4 className="flex items-center gap-2 font-medium text-foreground">
                <span className="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">
                  5
                </span>
                Article 5 | Data and privacy
              </h4>
              <p className="pl-7">
                This software is a self-hosted, open-source program. Its authors operate no centralized service and do
                not collect or upload your usage data. You control all data you generate, process, or access while using
                it and are responsible for its legality and security. You bear any consequences of improper data
                handling.
              </p>
            </section>

            <section className="space-y-1.5">
              <h4 className="flex items-center gap-2 font-medium text-foreground">
                <span className="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">
                  6
                </span>
                Article 6 | Compliance and legal responsibility
              </h4>
              <p className="pl-7">
                You are responsible for complying with all applicable laws and regulations in your country or region
                concerning cybersecurity, data security, personal information protection, computer crime, and related
                matters. In mainland China, these include, without limitation, the Cybersecurity Law, Data Security Law,
                Personal Information Protection Law, and relevant judicial interpretations.
                <span className="font-medium text-foreground">
                  {" "}
                  You alone bear all legal liability and consequences arising from your violation of those laws and
                  regulations or this Statement. The authors and contributors of this software bear no responsibility
                  for them.
                </span>
              </p>
            </section>

            <section className="space-y-1.5">
              <h4 className="flex items-center gap-2 font-medium text-foreground">
                <span className="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">
                  7
                </span>
                Article 7 | Disclaimer and limitation of liability
              </h4>
              <p className="pl-7">
                This software is provided "AS IS" and "AS AVAILABLE", without any express or implied warranties,
                including, without limitation, warranties of merchantability, fitness for a particular purpose,
                accuracy, or noninfringement. To the fullest extent permitted by applicable law, the authors and
                contributors are not liable for any direct, indirect, incidental, special, or consequential loss arising
                from the use of or inability to use this software, whether used properly or improperly, including,
                without limitation, data loss, system damage, business interruption, lost profits, or legal disputes.
              </p>
            </section>

            <section className="space-y-1.5">
              <h4 className="flex items-center gap-2 font-medium text-foreground">
                <span className="flex size-5 items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground text-xs">
                  8
                </span>
                Article 8 | Amendments and interpretation
              </h4>
              <p className="pl-7">
                The authors may update this Statement from time to time to reflect legal requirements or project
                development. Updated versions will be published with the project and take effect upon publication.
                Continued use of the software constitutes acceptance of the revised terms. To the extent permitted by
                law, the project authors retain final authority to interpret this Statement. If any provision is held
                invalid, the remaining provisions remain effective.
              </p>
            </section>
          </div>

          <DialogFooter className="mx-0 mb-0 flex-col items-stretch gap-2 rounded-b-xl px-6 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-muted-foreground text-xs">
              {readToEnd ? "You have viewed all terms" : "Scroll to the end of the terms before confirming"}
            </p>
            <DialogClose asChild>
              <Button
                type="button"
                disabled={!readToEnd}
                onClick={() => {
                  setAgreed(true);
                  setError("");
                }}
              >
                I have read and agree to all terms
              </Button>
            </DialogClose>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
