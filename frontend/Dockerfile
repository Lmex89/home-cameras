# React build stage
FROM node:20-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

# nginx serve stage (templates are envsubst'd by the official entrypoint;
# CAMERAS_PORT points at the Go backend's internal port)
FROM nginx:1.27-alpine
ENV CAMERAS_PORT=8004
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
EXPOSE 80
