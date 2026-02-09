package no.uio.ifi.localega.doa.services;

import com.networknt.schema.*;
import com.networknt.schema.Error;
import com.networknt.schema.regex.GraalJSRegularExpressionFactory;
import no.uio.ifi.localega.doa.exception.JsonSchemaValidationException;
import org.springframework.stereotype.Service;

import java.util.stream.Collectors;

@Service
public class JsonSchemaValidationService {

    private final Schema schema;

    public JsonSchemaValidationService() {
        SchemaRegistryConfig schemaRegistryConfig = SchemaRegistryConfig.builder()
                .regularExpressionFactory(GraalJSRegularExpressionFactory.getInstance()).build();

        SchemaRegistry schemaRegistry = SchemaRegistry.withDefaultDialect(SpecificationVersion.DRAFT_7,
                builder -> builder.schemaRegistryConfig(schemaRegistryConfig)
                        .schemaIdResolvers(resolvers -> resolvers
                                .mapPrefix("https://github.com/neicnordic/sensitive-data-archive/tree/main/sda-doa/src/main/resources",
                                        "classpath:schemas")));

        this.schema = schemaRegistry.getSchema(SchemaLocation.of(
                "https://github.com/neicnordic/sensitive-data-archive/tree/main/sda-doa/src/main/resources/export-request.json"
        ));


    }

    public void validate(String jsonContent) throws JsonSchemaValidationException {
        var errors = schema.validate(
                jsonContent,
                InputFormat.JSON,
                ctx -> ctx.executionConfig(c -> c.formatAssertionsEnabled(true))
        );

        if (!errors.isEmpty()) {
            String errorMessage = errors.stream()
                    .map(Error::getMessage)
                    .collect(Collectors.joining(", "));
            throw new JsonSchemaValidationException("JSON Schema Validation Failed: " + errorMessage);
        }
    }

}
