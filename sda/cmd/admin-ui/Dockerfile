# Use Node.js image
FROM node:22

# Set the working directory in the container
WORKDIR /usr/src/app

# Copy package.json and package-lock.json (if available) to install dependencies
COPY backend/package*.json ./

# Install dependencies
RUN npm install

# Copy the backend code into the container
COPY backend/ .

# Expose the backend port
EXPOSE 3000

# Start the backend server
CMD ["node", "index.js"]
