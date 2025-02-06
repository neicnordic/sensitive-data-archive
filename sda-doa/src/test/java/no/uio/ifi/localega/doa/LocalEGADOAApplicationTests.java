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
import java.util.UUID;

@Slf4j
class LocalEGADOAApplicationTests {

    private static String validToken;
    private static String invalidToken;
    private static String validVisaToken;
    private static String doaUrl;
    private static String mockauthUrl;
    private static String minioHost;

    @SneakyThrows
    @BeforeAll
    public static void setup() {
        doaUrl = System.getenv("DOA_URL");
        mockauthUrl = System.getenv("MOCKAUTH_URL");
        minioHost = System.getenv("MINIO_HOST");

        JSONArray tokens = Unirest.get(mockauthUrl + "/tokens").asJson().getBody().getArray();
        validToken = tokens.getString(0);
        invalidToken = tokens.getString(1);
        validVisaToken = tokens.getString(2);
    }

    @SneakyThrows
    @AfterEach
    public void tearDown() {
        File exportFolder = new File("outbox/requester@elixir-europe.org");
        if (exportFolder.exists() && exportFolder.isDirectory()) {
            FileUtils.deleteDirectory(exportFolder);
        }
    }

    @Test
    void testMetadataDatasetsNoToken() {
        int status = Unirest.get(doaUrl + "/metadata/datasets").asJson().getStatus();
        Assertions.assertEquals(HttpStatus.UNAUTHORIZED.value(), status);
    }

    @Test
    void testMetadataDatasetsInvalidToken() {
        int status = Unirest.get(doaUrl + "/metadata/datasets").header(HttpHeaders.AUTHORIZATION, "Bearer " + invalidToken).asJson().getStatus();
        Assertions.assertEquals(HttpStatus.UNAUTHORIZED.value(), status);
    }

    @Test
    void testMetadataDatasetsValidToken() {
        HttpResponse<JsonNode> response = Unirest.get(doaUrl + "/metadata/datasets").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).asJson();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.OK.value(), status);
        JSONArray datasets = response.getBody().getArray();
        Assertions.assertEquals(1, datasets.length());
        Assertions.assertEquals("EGAD00010000919", datasets.getString(0));
    }

    @Test
    void testMetadataFilesNoToken() {
        int status = Unirest.get(doaUrl + "/metadata/datasets/EGAD00010000919/files").asJson().getStatus();
        Assertions.assertEquals(HttpStatus.UNAUTHORIZED.value(), status);
    }

    @Test
    void testMetadataFilesInvalidToken() {
        int status = Unirest.get(doaUrl + "/metadata/datasets/EGAD00010000919/files").header(HttpHeaders.AUTHORIZATION, "Bearer " + invalidToken).asJson().getStatus();
        Assertions.assertEquals(HttpStatus.UNAUTHORIZED.value(), status);
    }

    @Test
    void testMetadataFilesValidTokenInvalidDataset() {
        HttpResponse<JsonNode> response = Unirest.get(doaUrl + "/metadata/datasets/EGAD00010000920/files").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).asJson();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.FORBIDDEN.value(), status);
    }

    @Test
    void testMetadataFilesValidTokenValidDataset() {
        HttpResponse<JsonNode> response = Unirest.get(doaUrl + "/metadata/datasets/EGAD00010000919/files").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).asJson();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.OK.value(), status);
        Assertions.assertEquals("[{\"fileId\":\"EGAF00000000014\",\"datasetId\":\"EGAD00010000919\",\"displayFileName\":\"body.enc\",\"fileName\":\"test/body.enc\",\"fileSize\":null,\"unencryptedChecksum\":null,\"unencryptedChecksumType\":null,\"decryptedFileSize\":null,\"decryptedFileChecksum\":null,\"decryptedFileChecksumType\":null,\"fileStatus\":\"READY\"}]", response.getBody().toString());
    }

    @Test
    void testStreamingNoToken() {
        int status = Unirest.get(doaUrl + "/files/EGAF00000000014").asJson().getStatus();
        Assertions.assertEquals(HttpStatus.UNAUTHORIZED.value(), status);
    }

    @Test
    void testStreamingInvalidToken() {
        int status = Unirest.get(doaUrl + "/files/EGAF00000000014").header(HttpHeaders.AUTHORIZATION, "Bearer " + invalidToken).asJson().getStatus();
        Assertions.assertEquals(HttpStatus.UNAUTHORIZED.value(), status);
    }

    @Test
    void testStreamingValidTokenInvalidFile() {
        HttpResponse<JsonNode> response = Unirest.get(doaUrl + "/files/EGAF00000000015").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).asJson();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.FORBIDDEN.value(), status);
    }

    @Test
    void testStreamingValidTokenValidFileFullPlain() {
        HttpResponse<byte[]> response = Unirest.get(doaUrl + "/files/EGAF00000000014").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).asBytes();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.OK.value(), status);
        Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(response.getBody()));
    }

    @Test
    void testStreamingValidTokenValidFileRangePlain() {
        HttpResponse<byte[]> response = Unirest.get(doaUrl + "/files/EGAF00000000014?startCoordinate=100&endCoordinate=200").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).asBytes();
        int status = response.getStatus();
        Assertions.assertEquals(HttpStatus.OK.value(), status);
        Assertions.assertEquals("09fbeae7cce2cd410b471b0a1a265fb53dc54c66c4c7c3111b8b9b95ac0e956f", DigestUtils.sha256Hex(response.getBody()));
    }

    @SneakyThrows
    @Test
    void testStreamingValidTokenValidFileFullEncrypted() {
        String publicKey = Files.readString(new File("test/crypt4gh/crypt4gh.pub.pem").toPath());
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/crypt4gh.sec.pem"), "password".toCharArray());
        HttpResponse<byte[]> response = Unirest.get("http://doa:8080/files/EGAF00000000014?destinationFormat=crypt4gh").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).header("Public-Key", publicKey).asBytes();
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
        String publicKey = Files.readString(new File("test/crypt4gh/crypt4gh.pub.pem").toPath());
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/crypt4gh.sec.pem"), "password".toCharArray());
        HttpResponse<byte[]> response = Unirest.get("http://doa:8080/files/EGAF00000000014?startCoordinate=100&endCoordinate=200&destinationFormat=crypt4gh").header(HttpHeaders.AUTHORIZATION, "Bearer " + validToken).header("Public-Key", publicKey).asBytes();
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
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = new FileInputStream("outbox/requester@elixir-europe.org/files/body.enc");
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
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = new FileInputStream("outbox/requester@elixir-europe.org/files/body.enc");
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
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/my.sec.pem"), "passw0rd".toCharArray());
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
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/my.sec.pem"), "passw0rd".toCharArray());
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
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/my.sec.pem"), "passw0rd".toCharArray());
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
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = new FileInputStream("outbox/requester@elixir-europe.org/files/body.enc");
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
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = new FileInputStream("outbox/requester@elixir-europe.org/files/body.enc");
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
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = new FileInputStream("outbox/requester@elixir-europe.org/files/body.enc");
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
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/my.sec.pem"), "passw0rd".toCharArray());
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
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/my.sec.pem"), "passw0rd".toCharArray());
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
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = new FileInputStream("outbox/requester@elixir-europe.org/files/body.enc");
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
        PrivateKey privateKey = KeyUtils.getInstance().readPrivateKey(new File("test/crypt4gh/my.sec.pem"), "passw0rd".toCharArray());
        try (InputStream byteArrayInputStream = getMinioClient().getObject(GetObjectArgs.builder().bucket("lega").object("requester@elixir-europe.org/body.enc").build());
             Crypt4GHInputStream crypt4GHInputStream = new Crypt4GHInputStream(byteArrayInputStream, privateKey)) {
            byte[] bytes = IOUtils.toByteArray(crypt4GHInputStream);
            Assertions.assertEquals("2aef808fb42fa7b1ba76cb16644773f9902a3fdc2569e8fdc049f38280c4577e", DigestUtils.sha256Hex(bytes));
        }
    }
    @SneakyThrows
    void export(String id, boolean dataset, String token) {
        String mqConnectionString = "amqps://guest:guest@rabbitmq:5671/sda";
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
                FileUtils.readFileToString(new File("test/crypt4gh/my.pub.pem"), Charset.defaultCharset()));
        channel.basicPublish("",
                "exportRequests",
                properties,
                message.getBytes());

        channel.close();
        connectionFactory.close();
        Thread.sleep(1000 * 3);
    }

    MinioClient getMinioClient() {
        return MinioClient.builder().endpoint(minioHost, 9000, false).region("us-west-1").credentials("minio", "miniostorage").build();
    }

}
