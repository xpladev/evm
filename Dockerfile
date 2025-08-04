FROM golang:1.24

# Install Node.js (LTS) and npm
RUN curl -fsSL https://deb.nodesource.com/setup_lts.x | bash - \
    && apt-get install -y nodejs jq \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /src

# Copy all source code into the image
COPY . .

RUN chmod +x ./local_node.sh
RUN ./local_node.sh --only-install

# Default command
CMD ["./local_node.sh", "-y"]