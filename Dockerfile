FROM golang:buster AS build
RUN curl -sL https://packages.microsoft.com/config/debian/10/packages-microsoft-prod.deb -o packages-microsoft-prod.deb && dpkg -i packages-microsoft-prod.deb
RUN curl -sL https://deb.nodesource.com/setup_14.x | bash -
RUN apt-get update && apt-get install apt-transport-https dotnet-sdk-3.1 nodejs -y
ADD . /src
RUN cd /src && ./utils/install.sh
RUN cd /src && ./utils/build.sh -v -tags csharp

FROM mcr.microsoft.com/dotnet/core/runtime:3.1.9-buster-slim
WORKDIR /app/cmd/bot
ENV LIBCOREFOLDER /usr/share/dotnet/shared/Microsoft.NETCore.App/3.1.9
COPY --from=build /src/web/static /app/web/static
COPY --from=build /src/web/views /app/web/views
COPY --from=build /src/cmd/bot/bot /app/cmd/bot/bot
COPY --from=build /src/migrations /app/migrations/
COPY --from=build /src/cmd/bot/*.dll /app/cmd/bot/
COPY --from=build /src/cmd/bot/charmap.bin.gz /app/cmd/bot/
RUN chmod 777 /app/cmd/bot/charmap.bin.gz
CMD ["./bot"]
