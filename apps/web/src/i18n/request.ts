// next-intl request config. Locale resolution follows: cookie → Accept-Language
// → default ("en"). v1 ships with English and Thai (section 2 — in scope);
// RTL support is reserved for future locales (section 10).

import { cookies, headers } from "next/headers";
import { getRequestConfig } from "next-intl/server";
import type { AbstractIntlMessages } from "next-intl";
import en from "./messages/en.json";
import th from "./messages/th.json";

const SUPPORTED = ["en", "th"] as const;
type Locale = (typeof SUPPORTED)[number];

const messageMap: Record<Locale, AbstractIntlMessages> = {
  en: en as AbstractIntlMessages,
  th: th as AbstractIntlMessages,
};

function isSupported(value: string | undefined): value is Locale {
  return !!value && (SUPPORTED as readonly string[]).includes(value);
}

export default getRequestConfig(async () => {
  const cookieStore = await cookies();
  const headerList = await headers();

  const cookieLocale = cookieStore.get("vl-locale")?.value;
  if (isSupported(cookieLocale)) {
    return { locale: cookieLocale, messages: messageMap[cookieLocale] };
  }

  const accept = headerList.get("accept-language") ?? "";
  const preferred = accept.split(",")[0]?.split("-")[0];
  const locale: Locale = isSupported(preferred) ? preferred : "en";
  return { locale, messages: messageMap[locale] };
});
