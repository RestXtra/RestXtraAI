import packageJson from "../../package.json";

const currentYear = new Date().getFullYear();

export const APP_CONFIG = {
  name: "RestXtra AI",
  version: packageJson.version,
  copyright: `© ${currentYear}, RestXtra AI.`,
  meta: {
    title: "RestXtra AI — 自动化分析协作平台",
    description: "LLM 驱动的基础设施分析与自动化协作平台",
  },
};
