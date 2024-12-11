package no.uio.ifi.localega.doa;

import com.rabbitmq.client.AMQP;
import com.rabbitmq.client.Channel;
import com.rabbitmq.client.ConnectionFactory;
import io.minio.GetObjectArgs;
import io.minio.MinioClient;
import kong.unirest.HttpResponse;
import kong.unirest.JsonNode;
import kong.unirest.Unirest;
import kong.unirest.json.JSONArray;
import lombok.SneakyThrows;
import lombok.extern.slf4j.Slf4j;
import no.elixir.crypt4gh.stream.Crypt4GHInputStream;
import no.elixir.crypt4gh.util.KeyUtils;
import org.apache.commons.codec.digest.DigestUtils;
import org.apache.commons.io.FileUtils;
import org.apache.commons.io.IOUtils;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Assertions;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;

import java.io.ByteArrayInputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.InputStream;
import java.nio.charset.Charset;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.security.PrivateKey;
import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.PreparedStatement;
import java.util.Properties;
import java.util.UUID;

@Slf4j
class LocalEGADOAApplicationTests {

    private static String validToken;
    private static String invalidToken;
    private static String validVisaToken;

    @SneakyThrows
    @BeforeAll
    public static void setup() {
        String url = String.format("jdbc:postgresql://%s:%s/%s", "localhost", "5432", "sda");
        Properties props = new Properties();
//        props.setProperty("user", "lega_in"); //will be used when lega_in user is set in db again
        props.setProperty("user", "postgres");
        props.setProperty("password", "rootpasswd");
        props.setProperty("ssl", "true");
        props.setProperty("application_name", "LocalEGA");
        props.setProperty("sslmode", "verify-full");
        props.setProperty("sslrootcert", new File("test/rootCA.pem").getAbsolutePath());
        props.setProperty("sslcert", new File("test/localhost-client.pem").getAbsolutePath());
        props.setProperty("sslkey", new File("test/localhost-client-key.der").getAbsolutePath());
        Connection connection = DriverManager.getConnection(url, props);
        PreparedStatement file = connection.prepareStatement("SELECT local_ega.insert_file('body.enc','requester@elixir-europe.org');");
        file.executeQuery();
        PreparedStatement header = connection.prepareStatement("UPDATE local_ega.files SET header = '637279707434676801000000010000006c00000000000000aa7ad1bb4f93bf5e4fb3bc28a95bc4d80bf2fd8075e69eb2ee15e0a4f08f1d78ab98c8fd9b50e675f71311936e8d0c6f73538962b836355d5d4371a12eae46addb43518b5236fb9554249710a473026f34b264a61d2ba52ed11abc1efa1d3478fa40a710' WHERE id = 1;");
        header.executeUpdate();
        PreparedStatement finalize = connection.prepareStatement("UPDATE local_ega.files SET archive_path = 'test/body.enc', status = 'READY', stable_id = 'EGAF00000000014' WHERE id = 1;");
        finalize.executeUpdate();
        connection.close();

//        props.setProperty("user", "lega_out"); //will be used when lega_out user is set in db again
        connection = DriverManager.getConnection(url, props);
        PreparedStatement dataset = connection.prepareStatement("INSERT INTO local_ega_ebi.filedataset(file_id, dataset_stable_id) values(1, 'EGAD00010000919');");
        dataset.executeUpdate();

        PreparedStatement dataset_event_registered = connection.prepareStatement(prepareInsertQueryDatasetEvent("EGAD00010000919", "registered", "mapping"));
        dataset_event_registered.executeUpdate();

        Thread.sleep(1000 * 3);

        PreparedStatement dataset_event_released = connection.prepareStatement(prepareInsertQueryDatasetEvent("EGAD00010000919", "released", "release"));
        dataset_event_released.executeUpdate();

        PreparedStatement datasetReferenceInsert = connection.prepareStatement("INSERT INTO sda.dataset_references(dataset_id, reference_id, reference_scheme) values('1', 'GDI-NO-10001','GDI');");
        datasetReferenceInsert.executeUpdate();
        connection.close();

        JSONArray tokens = Unirest.get("http://localhost:8000/tokens").asJson().getBody().getArray();
        validToken = tokens.getString(0);
        invalidToken = tokens.getString(1);
        validVisaToken = tokens.getString(2);
    }

    @SneakyThrows
    @AfterEach
    public void tearDown() {
        File exportFolder = new File("requester@elixir-europe.org");
        if (exportFolder.exists() && exportFolder.isDirectory()) {
            FileUtils.deleteDirectory(exportFolder);
        }
    }

    @Test
    void testMetadataDatasetsNoToken() {
        int status = Unirest.get("http://localhost:8080/metadata/datasets").asJson().getStatus();
        Assertions.assertEquals(HttpStatus.UNAUTHORIZED.value(), status);
    }

    @Test
    void testMetadataDatasetsInvalidToken() {
        int status = Unirest.get("http://localhost:8080/metadata/datasets").header(HttpHeaders.AUTHORIZATION, "Bearer " + invalidToken).asJson().getStatus();
        Assertions.assertEquals(HttpStatus.UNAUTHORIZED.value(), status);
    }

    @Test
    void testMetadataDatasetsValidToken() {
        HttpResponse<JsonNode> response = Unirest.get("http://localhost:8080/metadata/datasets").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).asJson();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.OK.value(), status);
        JSONArray datasets = response.getBody().getArray();
        Assertions.assertEquals(1, datasets.length());
        Assertions.assertEquals("EGAD00010000919", datasets.getString(0));
    }

    @Test
    void testMetadataFilesNoToken() {
        int status = Unirest.get("http://localhost:8080/metadata/datasets/EGAD00010000919/files").asJson().getStatus();
        Assertions.assertEquals(HttpStatus.UNAUTHORIZED.value(), status);
    }

    @Test
    void testMetadataFilesInvalidToken() {
        int status = Unirest.get("http://localhost:8080/metadata/datasets/EGAD00010000919/files").header(HttpHeaders.AUTHORIZATION, "Bearer " + invalidToken).asJson().getStatus();
        Assertions.assertEquals(HttpStatus.UNAUTHORIZED.value(), status);
    }

    @Test
    void testMetadataFilesValidTokenInvalidDataset() {
        HttpResponse<JsonNode> response = Unirest.get("http://localhost:8080/metadata/datasets/EGAD00010000920/files").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).asJson();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.FORBIDDEN.value(), status);
    }

    @Test
    void testMetadataFilesValidTokenValidDataset() {
        HttpResponse<JsonNode> response = Unirest.get("http://localhost:8080/metadata/datasets/EGAD00010000919/files").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).asJson();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.OK.value(), status);
        Assertions.assertEquals("[{\"fileId\":\"EGAF00000000014\",\"datasetId\":\"EGAD00010000919\",\"displayFileName\":\"body.enc\",\"fileName\":\"test/body.enc\",\"fileSize\":null,\"unencryptedChecksum\":null,\"unencryptedChecksumType\":null,\"decryptedFileSize\":null,\"decryptedFileChecksum\":null,\"decryptedFileChecksumType\":null,\"fileStatus\":\"READY\"}]", response.getBody().toString());
    }

    @Test
    void testStreamingNoToken() {
        int status = Unirest.get("http://localhost:8080/files/EGAF00000000014").asJson().getStatus();
        Assertions.assertEquals(HttpStatus.UNAUTHORIZED.value(), status);
    }

    @Test
    void testStreamingInvalidToken() {
        int status = Unirest.get("http://localhost:8080/files/EGAF00000000014").header(HttpHeaders.AUTHORIZATION, "Bearer " + invalidToken).asJson().getStatus();
        Assertions.assertEquals(HttpStatus.UNAUTHORIZED.value(), status);
    }

    @Test
    void testStreamingValidTokenInvalidFile() {
        HttpResponse<JsonNode> response = Unirest.get("http://localhost:8080/files/EGAF00000000015").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).asJson();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.FORBIDDEN.value(), status);
    }

    @Test
    void testStreamingValidTokenValidFileFullPlain() {
        HttpResponse<byte[]> response = Unirest.get("http://localhost:8080/files/EGAF00000000014").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).asBytes();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.OK.value(), status);
        Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(response.getBody()));
    }

    @Test
    void testStreamingValidTokenValidFileRangePlain() {
        HttpResponse<byte[]> response = Unirest.get("http://localhost:8080/files/EGAF00000000014?startCoordinate=100&endCoordinate=200").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).asBytes();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.OK.value(), status);
        Assertions.assertEquals("09fbeae7cce2cd410b471b0a1a265fb53dc54c66c4c7c3111b8b9b95ac0e956f", DigestUtils.sha256Hex(response.getBody()));
    }

    @SneakyThrows
    @Test
    void testStreamingValidTokenValidFileFullEncrypted() {
        String publicKey = Files.readString(new File("test/crypt4gh.pub.pem").toPath());
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh.sec.pem"), "password".toCharArray());
        HttpResponse<byte[]> response = Unirest.get("http://localhost:8080/files/EGAF00000000014?destinationFormat=crypt4gh").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).header("Public-Key", publicKey).asBytes();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.OK.value(), status);
        try (ByteArrayInputStream byteArrayInputStream = new ByteArrayInputStream(response.getBody());
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testStreamingValidTokenValidFileRangeEncrypted() {
        String publicKey = Files.readString(new File("test/crypt4gh.pub.pem").toPath());
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh.sec.pem"), "password".toCharArray());
        HttpResponse<byte[]> response = Unirest.get("http://localhost:8080/files/EGAF00000000014?startCoordinate=100&endCoordinate=200&destinationFormat=crypt4gh").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).header("Public-Key", publicKey).asBytes();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.OK.value(), status);
        try (ByteArrayInputStream byteArrayInputStream = new ByteArrayInputStream(response.getBody());
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("09fbeae7cce2cd410b471b0a1a265fb53dc54c66c4c7c3111b8b9b95ac0e956f", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testPOSIXExportRequestFileValidToken() {
        if (System.getenv("OUTBOX_TYPE").equals("S3")) {
            Assertions.assertTrue(true);
            return;
        }
        export("EGAF00000000014", false, validToken);
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = new FileInputStream("requester@elixir-europe.org/files/body.enc");
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testPOSIXExportRequestDatasetValidToken() {
        if (System.getenv("OUTBOX_TYPE").equals("S3")) {
            Assertions.assertTrue(true);
            return;
        }
        export("EGAD00010000919", true, validToken);
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = new FileInputStream("requester@elixir-europe.org/files/body.enc");
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testS3ExportRequestFileValidToken() {
        if (System.getenv("OUTBOX_TYPE").equals("POSIX")) {
            Assertions.assertTrue(true);
            return;
        }
        export("EGAF00000000014", false, validToken);
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = getMinioClient().getObject(GetObjectArgs.builder().bucket("lega").object("requester@elixir-europe.org/body.enc").build());
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testS3ExportRequestDatasetValidToken() {
        if (System.getenv("OUTBOX_TYPE").equals("POSIX")) {
            Assertions.assertTrue(true);
            return;
        }
        export("EGAD00010000919", true, validToken);
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = getMinioClient().getObject(GetObjectArgs.builder().bucket("lega").object("requester@elixir-europe.org/body.enc").build());
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testS3ExportRequestReferenceValidToken() {
        if (System.getenv("OUTBOX_TYPE").equals("POSIX")) {
            Assertions.assertTrue(true);
            return;
        }
        export("GDI-NO-10001", true, validToken);
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = getMinioClient().getObject(GetObjectArgs.builder().bucket("lega").object("requester@elixir-europe.org/body.enc").build());
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testPOSIXExportRequestReferenceValidToken() {
        if (System.getenv("OUTBOX_TYPE").equals("S3")) {
            Assertions.assertTrue(true);
            return;
        }
        export("GDI-NO-10001", true, validToken);
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = new FileInputStream("requester@elixir-europe.org/files/body.enc");
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testPOSIXExportRequestFileValidVisaToken() {
        if (System.getenv("OUTBOX_TYPE").equals("S3")) {
            Assertions.assertTrue(true);
            return;
        }
        export("EGAF00000000014", false, validVisaToken);
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = new FileInputStream("requester@elixir-europe.org/files/body.enc");
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testPOSIXExportRequestDatasetValidVisaToken() {
        if (System.getenv("OUTBOX_TYPE").equals("S3")) {
            Assertions.assertTrue(true);
            return;
        }
        export("EGAD00010000919", true, validVisaToken);
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = new FileInputStream("requester@elixir-europe.org/files/body.enc");
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testS3ExportRequestFileValidVisaToken() {
        if (System.getenv("OUTBOX_TYPE").equals("POSIX")) {
            Assertions.assertTrue(true);
            return;
        }
        export("EGAF00000000014", false, validVisaToken);
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = getMinioClient().getObject(GetObjectArgs.builder().bucket("lega").object("requester@elixir-europe.org/body.enc").build());
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testS3ExportRequestDatasetValidVisaToken() {
        if (System.getenv("OUTBOX_TYPE").equals("POSIX")) {
            Assertions.assertTrue(true);
            return;
        }
        export("EGAD00010000919", true, validVisaToken);
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = getMinioClient().getObject(GetObjectArgs.builder().bucket("lega").object("requester@elixir-europe.org/body.enc").build());
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testPOSIXExportRequestReferenceValidVisaToken() {
        if (System.getenv("OUTBOX_TYPE").equals("S3")) {
            Assertions.assertTrue(true);
            return;
        }
        export("GDI-NO-10001", true, validVisaToken);
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = new FileInputStream("requester@elixir-europe.org/files/body.enc");
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }

    @SneakyThrows
    @Test
    void testS3ExportRequestReferenceValidVisaToken() {
        if (System.getenv("OUTBOX_TYPE").equals("POSIX")) {
            Assertions.assertTrue(true);
            return;
        }
        export("GDI-NO-10001", true, validVisaToken);
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = getMinioClient().getObject(GetObjectArgs.builder().bucket("lega").object("requester@elixir-europe.org/body.enc").build());
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }
    @SneakyThrows
    void export(String id, boolean dataset, String token) {
        String mqConnectionString = "amqps://admin:guest@localhost:5671/sda";
        ConnectionFactory factory = new ConnectionFactory();
        factory.setUri(mqConnectionString);
        com.rabbitmq.client.Connection connectionFactory = factory.newConnection();
        Channel channel = connectionFactory.createChannel();
        AMQP.BasicProperties properties = new AMQP.BasicProperties()
                .builder()
                .deliveryMode(2)
                .contentType("application/json")
                .contentEncoding(StandardCharsets.UTF_8.displayName())
                .correlationId(UUID.randomUUID().toString())
                .build();

        String message = String.format("{\n" +
                        "\t\"jwtToken\" : \"%s\",\n" +
                        "\t\"%s\": \"%s\",\n" +
                        "\t\"publicKey\": \"%s\"\n" +
                        "}",
                token,
                dataset ? "datasetId" : "fileId",
                id,
                FileUtils.readFileToString(new File("test/my.pub.pem"), Charset.defaultCharset()));
        channel.basicPublish("",
                "exportRequests",
                properties,
                message.getBytes());

        channel.close();
        connectionFactory.close();
        Thread.sleep(1000 * 3);
    }

    MinioClient getMinioClient() {
        return MinioClient.builder().endpoint("localhost", 9000, false).region("us-west-1").credentials("minio", "miniostorage").build();
    }


    private static String prepareInsertQueryDatasetEvent(String datasetId, String event, String msgType) {
        return String.format("INSERT INTO sda.dataset_event_log(dataset_id, event, message) VALUES('%s','%s','{\"type\": \"%s\"}');", datasetId, event, msgType);
    }
}
