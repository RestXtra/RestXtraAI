/** @type {import('next').NextConfig} */

// 静态导出：`NEXT_EXPORT=1 next build` 产出纯静态目录到 web/out，可直接丢进
// nginx web 根目录运行。开发(next dev)不设该变量，保留 /api 反代与热更新。
const isExport = process.env.NEXT_EXPORT === "1";
// Vercel demo：整站走 mock，无后端，无需 /api 反代。
const isMock = process.env.NEXT_PUBLIC_MOCK === "1";

function deploymentConfig() {
  if (isExport) {
    return {
      // 纯静态导出：无 Node 运行时；图片不经优化；每个路由产出 <route>/index.html。
      output: "export",
      images: { unoptimized: true },
      trailingSlash: true,
    };
  }
  if (isMock) {
    return {
      // Vercel mock demo：无后端，不需要 /api 反代。
      images: { unoptimized: true },
    };
  }
  return {
    // 开发：把 /api/* 反代到 Go 后端（默认 :8787，可用 AUTOPENTEST_API 覆盖）。
    async rewrites() {
      const backend = process.env.AUTOPENTEST_API ?? "http://localhost:8787";
      return [{ source: "/api/:path*", destination: `${backend}/api/:path*` }];
    },
  };
}

const nextConfig = {
  reactCompiler: true,
  ...(isMock
    ? {
        // Demo builds use locked, self-hosted fonts so deployment does not
        // depend on fonts.googleapis.com being reachable.
        turbopack: {
          resolveAlias: {
            "@/lib/fonts/registry": "./src/lib/fonts/registry.mock.ts",
          },
        },
      }
    : {}),
  // 允许从局域网 IP 访问 dev 资源（HMR），按需增删。
  // dev 阶段放开任意 IPv4 来源访问 /_next/* 与 HMR（局域网 IP 变动也不受影响）。
  // 注意：Next 出于安全禁止裸 "*"，需用分段通配；"*.*.*.*" 匹配任意 IPv4。
  allowedDevOrigins: ["*.*.*.*"],
  compiler: {
    removeConsole: process.env.NODE_ENV === "production",
  },
  ...deploymentConfig(),
};

export default nextConfig;
