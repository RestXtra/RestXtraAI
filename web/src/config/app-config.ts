import packageJson from "../../package.json";

const currentYear = new Date().getFullYear();

export const APP_CONFIG = {
  name: "RestXtra AI",
  version: packageJson.version,
  copyright: `© ${currentYear}, RestXtra AI.`,
  meta: {
    title: "RestXtra AI — 自主渗透测试控制台",
    description: "LLM 驱动的自主渗透测试平台控制台",
  },
};
