FROM registry.access.redhat.com/ubi9/nodejs-20:latest AS build
USER 0
WORKDIR /build
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM registry.access.redhat.com/ubi9/nginx-124:latest
USER 0
COPY --from=build /build/dist /usr/share/nginx/html
COPY --from=build /build/deploy/nginx.conf /etc/nginx/nginx.conf
USER 1001
CMD ["nginx", "-g", "daemon off;"]
