# JAX - Jointed Admin eXaminator
The JAX is a replacement of the sda-admin cli in order to make it easier for the sda admins of each Elixir node.
This is a proof of concept(POC) developed in order to provide a starting point for the frontend of the sda admin.

## Description

The implementation is using a setup with vanilla Javascript, HTML and CSS. In order to be able to run the frontend the
admin-ui has been dockerized with a backend and frontend. In this POC we have:
* Used Bootstrap CSS, which is a well known CSS framework. The same version is used as the login in sda,  
downloaded files inside static folder
* Implemented a fetch function for the files table and a mock implementation for the users table
* Dockerized the project in order to test locally
* Checked accessibility and HTML5/CSS standards
* Added error handling and user feedback 

### Dependencies

* Docker
* npm

### Executing program

In order for this GUI to work you need to use two different repos.

#### 1. Inside the Starter kit storage and interfaces-repo
Clone this repo: https://github.com/GenomicDataInfrastructure/starter-kit-storage-and-interfaces/ . For testing purposes, you need to add some code at the bottom of the `load_data.sh`-file located in the `scripts`-folder:
```
## get correlation id from upload message
CORRID=$(
   curl -s -X POST \
       -H "content-type:application/json" \
       -u test:test http://rabbitmq:15672/api/queues/sda/inbox/get \
       -d '{"count":1,"encoding":"auto","ackmode":"ack_requeue_false"}' | jq -r .[0].properties.correlation_id
)
```
After that you run:
```
docker compose -f docker-compose-demo.yml up
```

**If** you run into problems starting the containers, you can try and remove volumes using this command:

```
docker compose -f docker-compose-demo.yml down --remove-orphans -v
```

After that you'll have to get a token that you will use fetching data. This can be obtained by using these commands:
```
token=$(curl -s -k https://localhost:8080/tokens | jq -r '.[0]')
```
This command will fetch an array of tokens and save the first token in a variable called `token`.

In order to see the token you just saved into the variable token you can run:
```
echo $token
```

Copy the token.

#### 2. Inside the Sensitive Data Archive-repo
Now move to the Sensitive Data Archive-repo: https://github.com/neicnordic/sensitive-data-archive/ Replace the hardcoded 'token' with the token that you copied from the other repo in the `index.js`-file.

``` 
function getToken() {
  return 'paste token here'; // Replace with your token
}
```
Then start up the docker containers by running
```
docker compose up
```
The backend runs on `localhost:3000` and the frontend on `localhost:5500`

If you run into problems take the containers down and delete volumes by running:
```
docker compose down -v
```

To start the containers again run:

```
docker compose up --build
```

If you click on `Files` in the tab you should see a table containing test data. If you view the Users-tab, you'll only see mock data for now.

## Authors

This POC was developed durin an Internal Hackathon in the sys dev team

## Acknowledgments

Helpful sites
* [Bootstrap 4.3 documentation](https://getbootstrap.com/docs/4.3/getting-started/introduction/)
* [HTML validator](https://validator.w3.org/)
* [CSS validator](https://jigsaw.w3.org/css-validator/)
* [WAVE Evaluation Tool](https://chromewebstore.google.com/detail/wave-evaluation-tool/jbbplnpkjmmeebjpijfedlgcdilocofh)
