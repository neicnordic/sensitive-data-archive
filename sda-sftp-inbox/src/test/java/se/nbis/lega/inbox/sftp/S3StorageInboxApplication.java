package se.nbis.lega.inbox.sftp;

import com.amazonaws.ClientConfiguration;
import com.amazonaws.Protocol;
import com.amazonaws.auth.AWSCredentials;
import com.amazonaws.auth.AWSStaticCredentialsProvider;
import com.amazonaws.auth.AnonymousAWSCredentials;
import com.amazonaws.client.builder.AwsClientBuilder;
import com.amazonaws.services.s3.AmazonS3;
import com.amazonaws.services.s3.AmazonS3ClientBuilder;
import com.google.gson.Gson;
import io.findify.s3mock.S3Mock;
import org.mockito.stubbing.Answer;
import org.springframework.amqp.core.MessagePostProcessor;
import org.springframework.amqp.rabbit.core.RabbitTemplate;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.SpringBootConfiguration;
import org.springframework.boot.autoconfigure.condition.ConditionalOnExpression;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.ComponentScan;
import org.springframework.context.annotation.FilterType;
import org.springframework.web.client.RestTemplate;
import se.nbis.lega.inbox.pojo.FileDescriptor;
import se.nbis.lega.inbox.s3.Synchronizer;

import java.util.concurrent.BlockingQueue;
import java.util.concurrent.LinkedBlockingQueue;

import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.doAnswer;
import static org.mockito.Mockito.mock;

/**
 * Test Spring Boot application's main class with some configuration and some beans defined.
 */
@ComponentScan(basePackages = "se.nbis.lega.inbox",
        excludeFilters = {@ComponentScan.Filter(type = FilterType.ASSIGNABLE_TYPE, classes = Synchronizer.class)})
@SpringBootConfiguration
public class S3StorageInboxApplication {

    private String exchange;
    private String routingKeyFiles;
    private String s3Region;
    private String s3Endpoint;
    private boolean useSSL;
    private String s3Bucket;

    private Gson gson;

    @ConditionalOnExpression("!'${inbox.s3.access-key}'.isEmpty() && !'${inbox.s3.secret-key}'.isEmpty() && !'${inbox.s3.bucket}'.isEmpty()")
    @Bean
    public AmazonS3 amazonS3() {
        new S3Mock.Builder().withPort(9000).withInMemoryBackend().build().start();
        AwsClientBuilder.EndpointConfiguration endpointConfiguration = new AwsClientBuilder.EndpointConfiguration(s3Endpoint, s3Region);
        AWSCredentials credentials = new AnonymousAWSCredentials();
        AWSStaticCredentialsProvider awsStaticCredentialsProvider = new AWSStaticCredentialsProvider(credentials);
        ClientConfiguration clientConfiguration = new ClientConfiguration();
        clientConfiguration.setProtocol(useSSL ? Protocol.HTTPS : Protocol.HTTP);
        return AmazonS3ClientBuilder.standard()
                .withEndpointConfiguration(endpointConfiguration)
                .withCredentials(awsStaticCredentialsProvider)
                .withClientConfiguration(clientConfiguration)
                .withPathStyleAccessEnabled(true)
                .build();
    }

    @Bean
    public RabbitTemplate rabbitTemplate() {
        RabbitTemplate mock = mock(RabbitTemplate.class);
        doAnswer((Answer<Void>) invocationOnMock -> {
            String routingKey = invocationOnMock.getArgument(1).toString();
            BlockingQueue<FileDescriptor> queue = routingKey.equals(routingKeyFiles) ? fileBlockingQueue() : hashBlockingQueue();
            queue.put(gson.fromJson(invocationOnMock.getArgument(2).toString(), FileDescriptor.class));
            return null;
        }).when(mock).convertAndSend(eq(exchange), anyString(), anyString(), any(MessagePostProcessor.class));
        return mock;
    }

    @Bean
    public RestTemplate restTemplate() {
        return mock(RestTemplate.class);
    }

    @Bean
    public BlockingQueue<FileDescriptor> fileBlockingQueue() {
        return new LinkedBlockingQueue<>();
    }

    @Bean
    public BlockingQueue<FileDescriptor> hashBlockingQueue() {
        return new LinkedBlockingQueue<>();
    }

    @Value("${inbox.mq.exchange}")
    public void setExchange(String exchange) {
        this.exchange = exchange;
    }

    @Value("${inbox.mq.routing-key.files}")
    public void setRoutingKeyFiles(String routingKeyFiles) {
        this.routingKeyFiles = routingKeyFiles;
    }

    @Value("${inbox.s3.region}")
    public void setS3Region(String s3Region) {
        this.s3Region = s3Region;
    }

    @Value("${inbox.s3.endpoint}")
    public void setS3Endpoint(String s3Endpoint) {
        this.s3Endpoint = s3Endpoint;
    }

    @Value("${inbox.s3.use-ssl}")
    public void setUseSSL(boolean useSSL) {
        this.useSSL = useSSL;
    }

    @Value("${inbox.s3.bucket}")
    public void setS3Bucket(String s3Bucket) {
        this.s3Bucket = s3Bucket;
    }

    @Autowired
    public void setGson(Gson gson) {
        this.gson = gson;
    }

}
