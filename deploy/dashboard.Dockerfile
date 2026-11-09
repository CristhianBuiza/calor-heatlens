# Build context: repository root (see docker-compose.yml).
FROM node:22-alpine AS build
WORKDIR /app
COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY dashboard/ ./
# `npm run build` also copies .next/static into the standalone bundle.
RUN npm run build

FROM node:22-alpine
WORKDIR /app
ENV NODE_ENV=production PORT=3000 HOSTNAME=0.0.0.0
COPY --from=build /app/.next/standalone ./
USER node
EXPOSE 3000
CMD ["node", "--max-old-space-size=192", "server.js"]
