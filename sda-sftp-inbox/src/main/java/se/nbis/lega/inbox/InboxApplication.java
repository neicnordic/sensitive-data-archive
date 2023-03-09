package se.nbis.lega.inbox;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.autoconfigure.security.servlet.SecurityAutoConfiguration;
import org.springframework.security.config.annotation.web.configuration.EnableWebSecurity;

/**
 * Spring Boot application's main class with some configuration and some beans defined.
 */
@EnableWebSecurity
@SpringBootApplication(exclude = SecurityAutoConfiguration.class)
public class InboxApplication {

    public static void main(String[] args) {
        SpringApplication.run(InboxApplication.class, args);
    }

}
