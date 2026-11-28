import { LocaleProvider } from "@/features/landing/i18n";
import { jsonLdScript, siteJsonLd } from "@/features/landing/structured-data";

export default async function LandingLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: jsonLdScript(siteJsonLd) }}
      />
      <div className="landing-light h-full overflow-x-hidden overflow-y-auto bg-white">
        <LocaleProvider initialLocale="zh-Hans">
          {children}
        </LocaleProvider>
      </div>
    </>
  );
}
